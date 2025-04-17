// Copyright (c) 2012, Suryandaru Triandana <syndtr@gmail.com>
// All rights reserved.
//
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package leveldb

import (
	"bytes"
	"sync"
	"sync/atomic"
	"time"

	"github.com/syndtr/goleveldb/leveldb/errors"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/storage"
)

var (
	errCompactionTransactExiting = errors.New("leveldb: compaction transact exiting")
)

type cStat struct {
	duration time.Duration
	read     int64
	write    int64
}

func (p *cStat) add(n *cStatStaging) {
	p.duration += n.duration
	p.read += n.read
	p.write += n.write
}

func (p *cStat) get() (duration time.Duration, read, write int64) {
	return p.duration, p.read, p.write
}

type cStatStaging struct {
	start    time.Time
	duration time.Duration
	on       bool
	read     int64
	write    int64
}

func (p *cStatStaging) startTimer() {
	if !p.on {
		p.start = time.Now()
		p.on = true
	}
}

func (p *cStatStaging) stopTimer() {
	if p.on {
		p.duration += time.Since(p.start)
		p.on = false
	}
}

type cStats struct {
	lk    sync.Mutex
	stats []cStat
}

func (p *cStats) addStat(level int, n *cStatStaging) {
	p.lk.Lock()
	if level >= len(p.stats) {
		newStats := make([]cStat, level+1)
		copy(newStats, p.stats)
		p.stats = newStats
	}
	p.stats[level].add(n)
	p.lk.Unlock()
}

func (p *cStats) getStat(level int) (duration time.Duration, read, write int64) {
	p.lk.Lock()
	defer p.lk.Unlock()
	if level < len(p.stats) {
		return p.stats[level].get()
	}
	return
}

func (db *DB) compactionError() {
	var err error
noerr:
	// No error.
	for {
		select {
		case err = <-db.compErrSetC:
			switch {
			case err == nil:
			case err == ErrReadOnly, errors.IsCorrupted(err):
				goto hasperr
			default:
				goto haserr
			}
		case <-db.closeC:
			return
		}
	}
haserr:
	// Transient error.
	for {
		select {
		case db.compErrC <- err:
		case err = <-db.compErrSetC:
			switch {
			case err == nil:
				goto noerr
			case err == ErrReadOnly, errors.IsCorrupted(err):
				goto hasperr
			default:
			}
		case <-db.closeC:
			return
		}
	}
hasperr:
	// Persistent error.
	for {
		select {
		case db.compErrC <- err:
		case db.compPerErrC <- err:
		case db.writeLockC <- struct{}{}:
			// Hold write lock, so that write won't pass-through.
			db.compWriteLocking = true
		case <-db.closeC:
			if db.compWriteLocking {
				// We should release the lock or Close will hang.
				<-db.writeLockC
			}
			return
		}
	}
}

type compactionTransactCounter int

func (cnt *compactionTransactCounter) incr() {
	*cnt++
}

type compactionTransactInterface interface {
	run(cnt *compactionTransactCounter) error
	revert() error
}

func (db *DB) compactionTransact(name string, t compactionTransactInterface) {
	defer func() {
		if x := recover(); x != nil {
			if x == errCompactionTransactExiting {
				if err := t.revert(); err != nil {
					db.logf("%s revert error %q", name, err)
				}
			}
			panic(x)
		}
	}()

	const (
		backoffMin = 1 * time.Second
		backoffMax = 8 * time.Second
		backoffMul = 2 * time.Second
	)
	var (
		backoff  = backoffMin
		backoffT = time.NewTimer(backoff)
		lastCnt  = compactionTransactCounter(0)

		disableBackoff = db.s.o.GetDisableCompactionBackoff()
	)
	for n := 0; ; n++ {
		// Check whether the DB is closed.
		if db.isClosed() {
			db.logf("%s exiting", name)
			db.compactionExitTransact()
		} else if n > 0 {
			db.logf("%s retrying N·%d", name, n)
		}

		// Execute.
		cnt := compactionTransactCounter(0)
		err := t.run(&cnt)
		if err != nil {
			db.logf("%s error I·%d %q", name, cnt, err)
		}

		// Set compaction error status.
		select {
		case db.compErrSetC <- err:
		case perr := <-db.compPerErrC:
			if err != nil {
				db.logf("%s exiting (persistent error %q)", name, perr)
				db.compactionExitTransact()
			}
		case <-db.closeC:
			db.logf("%s exiting", name)
			db.compactionExitTransact()
		}
		if err == nil {
			return
		}
		if errors.IsCorrupted(err) {
			db.logf("%s exiting (corruption detected)", name)
			db.compactionExitTransact()
		}

		if !disableBackoff {
			// Reset backoff duration if counter is advancing.
			if cnt > lastCnt {
				backoff = backoffMin
				lastCnt = cnt
			}

			// Backoff.
			backoffT.Reset(backoff)
			if backoff < backoffMax {
				backoff *= backoffMul
				if backoff > backoffMax {
					backoff = backoffMax
				}
			}
			select {
			case <-backoffT.C:
			case <-db.closeC:
				db.logf("%s exiting", name)
				db.compactionExitTransact()
			}
		}
	}
}

type compactionTransactFunc struct {
	runFunc    func(cnt *compactionTransactCounter) error
	revertFunc func() error
}

func (t *compactionTransactFunc) run(cnt *compactionTransactCounter) error {
	return t.runFunc(cnt)
}

func (t *compactionTransactFunc) revert() error {
	if t.revertFunc != nil {
		return t.revertFunc()
	}
	return nil
}

func (db *DB) compactionTransactFunc(name string, run func(cnt *compactionTransactCounter) error, revert func() error) {
	db.compactionTransact(name, &compactionTransactFunc{run, revert})
}

func (db *DB) compactionExitTransact() {
	panic(errCompactionTransactExiting)
}

func (db *DB) compactionCommit(name string, rec *sessionRecord) {
	db.compCommitLk.Lock()
	defer db.compCommitLk.Unlock() // Defer is necessary.
	db.compactionTransactFunc(name+"@commit", func(cnt *compactionTransactCounter) error {
		return db.s.commit(rec, true)
	}, nil)
}

func (db *DB) memCompaction() {
	mdb := db.getFrozenMem()
	if mdb == nil {
		return
	}
	defer mdb.decref()

	db.logf("memdb@flush N·%d S·%s", mdb.Len(), shortenb(int64(mdb.Size())))

	// Don't compact empty memdb.
	if mdb.Len() == 0 {
		db.logf("memdb@flush skipping")
		// drop frozen memdb
		db.dropFrozenMem()
		return
	}

	// Pause table compaction.
	resumeC := make(chan struct{})
	select {
	case db.tcompPauseC <- (chan<- struct{})(resumeC):
	case <-db.compPerErrC:
		close(resumeC)
		resumeC = nil
	case <-db.closeC:
		db.compactionExitTransact()
	}

	var (
		rec        = &sessionRecord{}
		stats      = &cStatStaging{}
		flushLevel int
	)

	// Generate tables.
	db.compactionTransactFunc("memdb@flush", func(cnt *compactionTransactCounter) (err error) {
		stats.startTimer()
		flushLevel, err = db.s.flushMemdb(rec, mdb.DB, db.memdbMaxLevel)
		stats.stopTimer()
		return
	}, func() error {
		for _, r := range rec.addedTables {
			db.logf("memdb@flush revert @%d", r.num)
			if err := db.s.stor.Remove(storage.FileDesc{Type: storage.TypeTable, Num: r.num}); err != nil {
				return err
			}
		}
		return nil
	})

	rec.setJournalNum(db.journalFd.Num)
	rec.setSeqNum(db.frozenSeq)

	// Commit.
	stats.startTimer()
	db.compactionCommit("memdb", rec)
	stats.stopTimer()

	db.logf("memdb@flush committed F·%d T·%v", len(rec.addedTables), stats.duration)

	// Save compaction stats
	for _, r := range rec.addedTables {
		stats.write += r.size
	}
	db.compStats.addStat(flushLevel, stats)
	atomic.AddUint32(&db.memComp, 1)

	// Drop frozen memdb.
	db.dropFrozenMem()

	// Resume table compaction.
	if resumeC != nil {
		select {
		case <-resumeC:
			close(resumeC)
		case <-db.closeC:
			db.compactionExitTransact()
		}
	}

	// Trigger table compaction.
	db.compTrigger(db.tcompCmdC)
}

type biIndexer struct {
	offset, keyLen, valLen int
	canStop, shouldStop    bool
}

type bufferedIter struct {
	data     []byte
	indexers []biIndexer
	p        int
}

func (bi *bufferedIter) Reset() {
	bi.data = bi.data[:0]
	bi.indexers = bi.indexers[:0]
	bi.p = -1
}

func (bi *bufferedIter) Append(k, v []byte, canStop, shouldStop bool) int {
	bi.indexers = append(bi.indexers, biIndexer{
		offset:     len(bi.data),
		keyLen:     len(k),
		valLen:     len(v),
		canStop:    canStop,
		shouldStop: shouldStop,
	})
	bi.data = append(bi.data, k...)
	bi.data = append(bi.data, v...)
	return len(bi.data)
}

func (bi *bufferedIter) Next() bool {
	if bi.p+1 < len(bi.indexers) {
		bi.p++
		return true
	}
	return false
}

func (bi *bufferedIter) Key() []byte {
	idx := &bi.indexers[bi.p]
	return bi.data[idx.offset:][:idx.keyLen]
}

func (bi *bufferedIter) Value() []byte {
	idx := &bi.indexers[bi.p]
	return bi.data[idx.offset+idx.keyLen:][:idx.valLen]
}

func (bi *bufferedIter) CanStop() bool {
	return bi.indexers[bi.p].canStop
}

func (bi *bufferedIter) ShouldStop() bool {
	return bi.indexers[bi.p].shouldStop
}

var bufferedIterPool = sync.Pool{
	New: func() interface{} { return &bufferedIter{} },
}

type tableCompactionBuilder struct {
	db    *DB
	s     *session
	c     *compaction
	rec   *sessionRecord
	stat1 *cStatStaging

	kerrCnt int
	dropCnt int

	minSeq    uint64
	strict    bool
	tableSize int
}

func (b *tableCompactionBuilder) flushTW(tw *tWriter) error {
	t, err := tw.finish()
	if err != nil {
		return err
	}
	b.rec.addTableFile(b.c.sourceLevel+1, t)
	b.stat1.write += t.size
	b.s.logf("table@build created L%d@%d N·%d S·%s %q:%q", b.c.sourceLevel+1, t.fd.Num, tw.tw.EntriesLen(), shortenb(t.size), t.imin, t.imax)
	return nil
}

func (b *tableCompactionBuilder) writeLoop(iterC <-chan *bufferedIter) (err error) {
	twC := make(chan *tWriter, 8)
	errC := make(chan error)

	go func() {
		errC <- b.flushLoop(twC)
		close(errC)
	}()

	var tw *tWriter
	defer func() {
		close(twC)
		//cleanup
		if tw != nil {
			if derr := tw.drop(); derr != nil {
				if err == nil {
					err = derr
				}
			}
			tw = nil
		}

		if werr := <-errC; werr != nil {
			if err == nil {
				err = werr
			}
		}
	}()

	for iter := range iterC {
		for iter.Next() {
			// flush if needed
			if tw != nil && iter.CanStop() && (iter.ShouldStop() || tw.tw.BytesLen() >= b.tableSize) {
				select {
				case twC <- tw:
					tw = nil
				case err = <-errC:
					return err
				}
			}

			// Create new table if not already.
			if tw == nil {
				// Create new table.
				var err error
				if tw, err = b.s.tops.create(); err != nil {
					return err
				}
			}
			// Write key/value into table.
			if err := tw.append(iter.Key(), iter.Value()); err != nil {
				return err
			}
		}
		bufferedIterPool.Put(iter)
	}
	// Finish last table.
	if tw != nil && !tw.empty() {
		select {
		case twC <- tw:
			tw = nil
		case err = <-errC:
			return err
		}
	}
	return nil
}

func (b *tableCompactionBuilder) flushLoop(twC <-chan *tWriter) (err error) {
	defer func() {
		for tw := range twC {
			if derr := tw.drop(); derr != nil {
				if err == nil {
					err = derr
				}
			}
		}
	}()
	for tw := range twC {
		t, err := tw.finish()
		if err != nil {
			return err
		}

		b.rec.addTableFile(b.c.sourceLevel+1, t)
		b.stat1.write += t.size
		b.s.logf("table@build created L%d@%d N·%d S·%s %q:%q", b.c.sourceLevel+1, t.fd.Num, tw.tw.EntriesLen(), shortenb(t.size), t.imin, t.imax)
	}
	return nil
}

func (b *tableCompactionBuilder) run(cnt *compactionTransactCounter) (err error) {
	var (
		hasLastUkey    bool // The key might has zero length, so this is necessary.
		lastUkey       []byte
		lastSeq        uint64
		bIter          *bufferedIter
		iterC          = make(chan *bufferedIter, 16)
		errC           = make(chan error)
		oPrefix        = b.s.o.GetOverflowPrefix()
		lastHasOPrefix bool
	)

	// Restore compaction state.
	b.c.restore()

	go func() {
		errC <- b.writeLoop(iterC)
		close(errC)
	}()

	defer func() {
		close(iterC)
		if werr := <-errC; werr != nil {
			if err == nil {
				err = werr
			}
		}

		// remove all added tables on error
		if err != nil {
			for _, at := range b.rec.addedTables {
				b.s.stor.Remove(storage.FileDesc{Type: storage.TypeTable, Num: at.num})
			}
			b.rec.resetAddedTables()
		}
	}()

	b.stat1.startTimer()
	defer b.stat1.stopTimer()

	iter := b.c.newIterator()
	defer iter.Release()
	for i := 0; iter.Next(); i++ {
		// Incr transact counter.
		cnt.incr()

		ikey := iter.Key()
		ukey, seq, kt, kerr := parseInternalKey(ikey)
		canStop := false
		shouldStop := false
		if kerr == nil {
			shouldStop = b.c.shouldStopBefore(ikey)

			if !hasLastUkey || b.s.icmp.uCompare(lastUkey, ukey) != 0 {
				// First occurrence of this user key.

				hasOPrefix := len(oPrefix) > 0 && bytes.HasPrefix(ukey, oPrefix)
				if hasLastUkey && hasOPrefix != lastHasOPrefix {
					shouldStop = true
				}
				// Only rotate tables if ukey doesn't hop across.
				canStop = true

				hasLastUkey = true
				lastHasOPrefix = hasOPrefix
				lastUkey = append(lastUkey[:0], ukey...)
				lastSeq = keyMaxSeq
			}

			switch {
			case lastSeq <= b.minSeq:
				// Dropped because newer entry for same user key exist
				fallthrough // (A)
			case kt == keyTypeDel && seq <= b.minSeq && b.c.baseLevelForKey(lastUkey):
				// For this user key:
				// (1) there is no data in higher levels
				// (2) data in lower levels will have larger seq numbers
				// (3) data in layers that are being compacted here and have
				//     smaller seq numbers will be dropped in the next
				//     few iterations of this loop (by rule (A) above).
				// Therefore this deletion marker is obsolete and can be dropped.
				lastSeq = seq
				b.dropCnt++
				continue
			default:
				lastSeq = seq
			}
		} else {
			if b.strict {
				return kerr
			}

			// Don't drop corrupted keys.
			hasLastUkey = false
			lastUkey = lastUkey[:0]
			lastSeq = keyMaxSeq
			b.kerrCnt++
		}

		if bIter == nil {
			bIter = bufferedIterPool.Get().(*bufferedIter)
			bIter.Reset()
		}

		if bIter.Append(ikey, iter.Value(), canStop, shouldStop) >= b.s.o.GetBlockSize() {
			// Check for pause event.
			if b.db != nil {
				select {
				case ch := <-b.db.tcompPauseC:
					b.db.pauseCompaction(ch)
				case <-b.db.closeC:
					b.db.compactionExitTransact()
				default:
				}
			}

			select {
			case iterC <- bIter:
				bIter = nil
			case err := <-errC:
				return err
			}
		}
	}

	if err := iter.Error(); err != nil {
		return err
	}

	if bIter != nil {
		select {
		case iterC <- bIter:
			bIter = nil
		case err := <-errC:
			return err
		}
	}
	return nil
}

func (b *tableCompactionBuilder) revert() error {
	for _, at := range b.rec.addedTables {
		b.s.logf("table@build revert @%d", at.num)
		if err := b.s.stor.Remove(storage.FileDesc{Type: storage.TypeTable, Num: at.num}); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) tableCompaction(c *compaction, noTrivial bool) {
	defer c.release()

	rec := &sessionRecord{}
	if !c.overflowed {
		rec.addCompPtr(c.sourceLevel, c.imax)
	}

	if !noTrivial && c.trivial() {
		t := c.levels[0][0]
		db.logf("table@move L%d@%d -> L%d", c.sourceLevel, t.fd.Num, c.sourceLevel+1)
		rec.delTable(c.sourceLevel, t.fd.Num)
		rec.addTableFile(c.sourceLevel+1, t)
		db.compactionCommit("table-move", rec)
		return
	}

	var stats [2]cStatStaging
	for i, tables := range c.levels {
		for _, t := range tables {
			stats[i].read += t.size
			// Insert deleted tables into record
			rec.delTable(c.sourceLevel+i, t.fd.Num)
		}
	}
	sourceSize := stats[0].read + stats[1].read
	minSeq := db.minSeq()
	db.logf("table@compaction L%d·%d -> L%d·%d S·%s Q·%d", c.sourceLevel, len(c.levels[0]), c.sourceLevel+1, len(c.levels[1]), shortenb(sourceSize), minSeq)

	b := &tableCompactionBuilder{
		db:        db,
		s:         db.s,
		c:         c,
		rec:       rec,
		stat1:     &stats[1],
		minSeq:    minSeq,
		strict:    db.s.o.GetStrict(opt.StrictCompaction),
		tableSize: db.s.o.GetCompactionTableSize(c.sourceLevel + 1),
	}
	db.compactionTransact("table@build", b)

	// Commit.
	stats[1].startTimer()
	db.compactionCommit("table", rec)
	stats[1].stopTimer()

	resultSize := stats[1].write
	db.logf("table@compaction committed F%s S%s Ke·%d D·%d T·%v", sint(len(rec.addedTables)-len(rec.deletedTables)), sshortenb(resultSize-sourceSize), b.kerrCnt, b.dropCnt, stats[1].duration)

	// Save compaction stats
	for i := range stats {
		db.compStats.addStat(c.sourceLevel+1, &stats[i])
	}
	switch c.typ {
	case level0Compaction:
		atomic.AddUint32(&db.level0Comp, 1)
	case nonLevel0Compaction:
		atomic.AddUint32(&db.nonLevel0Comp, 1)
	case seekCompaction:
		atomic.AddUint32(&db.seekComp, 1)
	}
}

func (db *DB) tableRangeCompaction(level int, umin, umax []byte) error {
	db.logf("table@compaction range L%d %q:%q", level, umin, umax)
	if level >= 0 {
		if c := db.s.getCompactionRange(level, umin, umax, true); c != nil {
			db.tableCompaction(c, true)
		}
	} else {
		// Retry until nothing to compact.
		for {
			compacted := false

			// Scan for maximum level with overlapped tables.
			v := db.s.version()
			m := 1
			for i := m; i < len(v.levels); i++ {
				tables := v.levels[i]
				if tables.overlaps(db.s.icmp, umin, umax, false) {
					m = i
				}
			}
			v.release()

			for level := 0; level < m; level++ {
				if c := db.s.getCompactionRange(level, umin, umax, false); c != nil {
					db.tableCompaction(c, true)
					compacted = true
				}
			}

			if !compacted {
				break
			}
		}
	}

	return nil
}

func (db *DB) tableAutoCompaction() {
	if c := db.s.pickCompaction(); c != nil {
		db.tableCompaction(c, false)
	}
}

func (db *DB) tableNeedCompaction() bool {
	v := db.s.version()
	defer v.release()
	return v.needCompaction()
}

// resumeWrite returns an indicator whether we should resume write operation if enough level0 files are compacted.
func (db *DB) resumeWrite() bool {
	v := db.s.version()
	defer v.release()
	return v.tLen(0) < db.s.o.GetWriteL0PauseTrigger()
}

func (db *DB) pauseCompaction(ch chan<- struct{}) {
	select {
	case ch <- struct{}{}:
	case <-db.closeC:
		db.compactionExitTransact()
	}
}

type cCmd interface {
	ack(err error)
}

type cAuto struct {
	// Note for table compaction, an non-empty ackC represents it's a compaction waiting command.
	ackC chan<- error
}

func (r cAuto) ack(err error) {
	if r.ackC != nil {
		defer func() {
			_ = recover()
		}()
		r.ackC <- err
	}
}

type cRange struct {
	level    int
	min, max []byte
	ackC     chan<- error
}

func (r cRange) ack(err error) {
	if r.ackC != nil {
		defer func() {
			_ = recover()
		}()
		r.ackC <- err
	}
}

// This will trigger auto compaction but will not wait for it.
func (db *DB) compTrigger(compC chan<- cCmd) {
	select {
	case compC <- cAuto{}:
	default:
	}
}

// This will trigger auto compaction and/or wait for all compaction to be done.
func (db *DB) compTriggerWait(compC chan<- cCmd) (err error) {
	ch := make(chan error)
	defer close(ch)
	// Send cmd.
	select {
	case compC <- cAuto{ch}:
	case err = <-db.compErrC:
		return
	case <-db.closeC:
		return ErrClosed
	}
	// Wait cmd.
	select {
	case err = <-ch:
	case err = <-db.compErrC:
	case <-db.closeC:
		return ErrClosed
	}
	return err
}

// Send range compaction request.
func (db *DB) compTriggerRange(compC chan<- cCmd, level int, min, max []byte) (err error) {
	ch := make(chan error)
	defer close(ch)
	// Send cmd.
	select {
	case compC <- cRange{level, min, max, ch}:
	case err := <-db.compErrC:
		return err
	case <-db.closeC:
		return ErrClosed
	}
	// Wait cmd.
	select {
	case err = <-ch:
	case err = <-db.compErrC:
	case <-db.closeC:
		return ErrClosed
	}
	return err
}

func (db *DB) mCompaction() {
	var x cCmd

	defer func() {
		if x := recover(); x != nil {
			if x != errCompactionTransactExiting {
				panic(x)
			}
		}
		if x != nil {
			x.ack(ErrClosed)
		}
		db.closeW.Done()
	}()

	for {
		select {
		case x = <-db.mcompCmdC:
			switch x.(type) {
			case cAuto:
				db.memCompaction()
				x.ack(nil)
				x = nil
			default:
				panic("leveldb: unknown command")
			}
		case <-db.closeC:
			return
		}
	}
}

func (db *DB) tCompaction() {
	var (
		x     cCmd
		waitQ []cCmd
	)

	defer func() {
		if x := recover(); x != nil {
			if x != errCompactionTransactExiting {
				panic(x)
			}
		}
		for i := range waitQ {
			waitQ[i].ack(ErrClosed)
			waitQ[i] = nil
		}
		if x != nil {
			x.ack(ErrClosed)
		}
		db.closeW.Done()
	}()

	for {
		if db.tableNeedCompaction() {
			select {
			case x = <-db.tcompCmdC:
			case ch := <-db.tcompPauseC:
				db.pauseCompaction(ch)
				continue
			case <-db.closeC:
				return
			default:
			}
			// Resume write operation as soon as possible.
			if len(waitQ) > 0 && db.resumeWrite() {
				for i := range waitQ {
					waitQ[i].ack(nil)
					waitQ[i] = nil
				}
				waitQ = waitQ[:0]
			}
		} else {
			for i := range waitQ {
				waitQ[i].ack(nil)
				waitQ[i] = nil
			}
			waitQ = waitQ[:0]
			select {
			case x = <-db.tcompCmdC:
			case ch := <-db.tcompPauseC:
				db.pauseCompaction(ch)
				continue
			case <-db.closeC:
				return
			}
		}
		if x != nil {
			switch cmd := x.(type) {
			case cAuto:
				if cmd.ackC != nil {
					// Check the write pause state before caching it.
					if db.resumeWrite() {
						x.ack(nil)
					} else {
						waitQ = append(waitQ, x)
					}
				}
			case cRange:
				x.ack(db.tableRangeCompaction(cmd.level, cmd.min, cmd.max))
			default:
				panic("leveldb: unknown command")
			}
			x = nil
		}
		db.tableAutoCompaction()
	}
}
