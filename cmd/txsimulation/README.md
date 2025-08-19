# Transaction Simulator

## Description

The transaction simulator supports execution against both local networks (NetworkHub) and the Hayabusa devnet with realistic synthetic activity that simulates the behavior of validators and delegators in a real network.

## Current Features

- Spins up a local network using networkhub
- Seeds the network with validators and delegators
- Processes random lifecycles for each and generates more as chain progresses
- Support for Hayabusa devnet with synthetic activity

## Implemented Features

### Validators
- **Entry**: Entry of new validators to the network
- **Exit**: Exit of active validators
- **Withdrawal**: Withdrawal of validator funds
- **Increase**: Increase of active validator stake
- **Decrease**: Decrease of active validator stake
- **Queue**: Queuing of validators for activation

### Delegators
- **Entry**: Entry of new delegators
- **Exit**: Exit of active delegators

## How to Use

### Run against Hayabusa Devnet

```bash
# Connect to Hayabusa devnet with custom genesis URL
go run ./cmd/txsimulation --devnet https://hayabusa.live.dev.node.vechain.org/ --genesis-url https://vechain.github.io/thor-hayabusa/genesis.json
```

### Run against NetworkHub (Local Network)

```bash
# Run against local network
go run ./cmd/txsimulation --networkhub
```

### Basic Usage

```bash
go run ./cmd/txsimulation
```

### Available Flags

- `--networkhub`: Run against local NetworkHub (default)
- `--devnet <URL>`: Run against a devnet at the specified URL
- `--genesis-url <URL>`: Specify custom genesis.json URL (only used with --devnet, defaults to Hayabusa genesis)

**Note**: The `--genesis-url` flag is only used when `--devnet` is specified. It allows you to use a custom genesis.json file instead of the default Hayabusa genesis.

## Synthetic Activity

### Initialization
- **All Available Validators**: Uses all validators from the genesis.json (typically 11 for Hayabusa)
  - First third: Long-term validators (200-500 periods)
  - Second third: Medium-term validators (50-150 periods)
  - Last third: Short-term validators (10-40 periods)

- **15 Delegators**: With different delegation strategies
  - 5 long-term delegators (100-300 periods)
  - 5 medium-term delegators (30-80 periods)
  - 5 short-term delegators (10-30 periods)

### Continuous Activity
The simulator generates continuous activity every 30 seconds with the following probabilities:

- **15%**: New validators
- **20%**: New delegators
- **15%**: Increase validator stake
- **15%**: Decrease validator stake
- **15%**: Validator exits
- **15%**: Delegator exits
- **5%**: No activity

## Staking Strategies

### Validators
1. **Long Term**: 100-500 periods, no entry delay
2. **Medium Term**: 30-100 periods, 1-10 block delay
3. **Short Term**: 6-30 periods, 5-20 block delay
4. **With Delay**: 20-80 periods, 1-5 epoch delay

### Delegators
1. **Long Term**: 50-200 periods, no entry delay
2. **Medium Term**: 20-50 periods, 1-15 block delay
3. **Short Term**: 6-20 periods, 5-25 block delay

## Monitoring and Logs

### Lifecycle States
- **Pending**: Waiting to be processed
- **Queued**: Queued for activation
- **Active**: Active in the network
- **Exit Signalled**: Exit signal sent
- **Withdrawn**: Completely withdrawn

### Reports
Reports are automatically generated in:
```
fullnet-output/lifecycle-{timestamp}.txt
```

### Real-time Logs
The simulator shows detailed logs of:
- Validator and delegator status
- Entry/exit operations
- Stake changes
- Generated synthetic activity

## Network Configuration

### Hayabusa Devnet Parameters
The configuration is dynamically loaded from the official Hayabusa genesis.json at [https://vechain.github.io/thor-hayabusa/genesis.json](https://vechain.github.io/thor-hayabusa/genesis.json):

- **Validators**: Loaded from the `authority` section of genesis.json
- **Transition Period**: Extracted from `forkConfig.HAYABUSA_TP`
- **Network Parameters**: Based on the actual Hayabusa devnet configuration
- **Real Validators**: Uses the actual validator addresses and identities from the genesis file

The simulator automatically fetches and parses the genesis.json to ensure it's using the most up-to-date configuration for the Hayabusa devnet.

## Use Cases

- **Capture metrics**
- **Loading testing**
- **Local development & debugging**
- **Devnet testing and validation**
- **Realistic network simulation**

### Load Testing
- Simulates realistic load on the devnet
- Tests processing capacity
- Validates behavior under stress

### Development and Debugging
- Tests new features in real environment
- Network problem debugging
- Smart contract validation

### Metrics Analysis
- Captures performance metrics
- Usage pattern analysis
- Network parameter optimization

## Considerations

### Accounts and Funds
- You need accounts with VET on the devnet
- Accounts must have staking permissions
- Use specific testing accounts

### Shared Network
- The devnet is shared with other users
- Activity may affect other participants
- Consider the impact on the testing environment

### Limitations
- Less control than NetworkHub
- Dependency on devnet availability
- Possible conflicts with other users


## Future Features

- Enhanced validator stake management
- More sophisticated delegation strategies
- Advanced metrics and analytics
- Support for additional networks
