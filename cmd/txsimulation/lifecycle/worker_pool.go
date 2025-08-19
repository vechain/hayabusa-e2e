package lifecycle

import (
	"sync"
)

type WorkerPool struct {
	maxWorkers int
	jobQueue   chan Worker
	wg         sync.WaitGroup
	once       sync.Once
	started    bool
	mu         sync.Mutex
}

func NewWorkerPool(maxWorkers int) *WorkerPool {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	return &WorkerPool{
		maxWorkers: maxWorkers,
		jobQueue:   make(chan Worker, maxWorkers*2), // Buffer to prevent blocking
	}
}

type Worker = func()

// Start initializes the worker pool with the specified number of worker goroutines
func (wp *WorkerPool) Start() {
	wp.once.Do(func() {
		wp.mu.Lock()
		wp.started = true
		wp.mu.Unlock()

		for i := 0; i < wp.maxWorkers; i++ {
			go wp.worker()
		}
	})
}

// worker is the main worker goroutine that processes jobs from the queue
func (wp *WorkerPool) worker() {
	for job := range wp.jobQueue {
		job() // Execute the job (ignoring error for now, could be enhanced)
		wp.wg.Done()
	}
}

// Run submits a single worker job to the pool
func (wp *WorkerPool) Run(worker Worker) {
	wp.Start() // Ensure workers are started
	wp.wg.Add(1)
	wp.jobQueue <- worker
}

// RunBatch submits multiple worker jobs to the pool
func (wp *WorkerPool) RunBatch(workers []Worker) {
	wp.Start() // Ensure workers are started
	for _, worker := range workers {
		wp.wg.Add(1)
		wp.jobQueue <- worker
	}
}

// Wait waits for all submitted jobs to complete
func (wp *WorkerPool) Wait() {
	wp.wg.Wait()
}

// RunAndWait submits a single job and waits for it to complete
func (wp *WorkerPool) RunAndWait(worker Worker) {
	wp.Run(worker)
	wp.Wait()
}

// RunBatchAndWait submits multiple jobs and waits for all to complete
func (wp *WorkerPool) RunBatchAndWait(workers []Worker) {
	wp.RunBatch(workers)
	wp.Wait()
}

// Close shuts down the worker pool
func (wp *WorkerPool) Close() {
	wp.mu.Lock()
	defer wp.mu.Unlock()

	if wp.started {
		close(wp.jobQueue)
	}
}
