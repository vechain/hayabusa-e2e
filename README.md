# Hayabusa E2E

## Overview

Hayabusa E2E is a testing suite for the Hayabusa fork of `vechain/thor`. It leverages tools such as [draupnir](https://github.com/vechain/draupnir) and [networkhub](https://github.com/vechain/networkhub) to create a comprehensive testing environment. The suite is designed to facilitate the testing of various features and functionalities of the Hayabusa fork.

## Usage

It allows easy testing of local `hayabusa` repos by setting the `THOR_WORKING_DIR` environment variable.

Eg (replace `tests/validations` by your own test directory, for instance `tests/delegations`):

```bash
export THOR_WORKING_DIR=/path/to/your/hayabusa
go test -v -failfast -timeout 3600s ./tests/validations
```
