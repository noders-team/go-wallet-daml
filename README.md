[![Go Version](https://img.shields.io/badge/Go-1.23+-blue.svg)](https://golang.org/)
[![Build Status](https://img.shields.io/badge/build-passing-green.svg)]()

# Go Wallet DAML SDK

A comprehensive Go SDK for building wallet applications on DAML Canton ledgers with Splice Amulet protocol support.

## Overview

The Go Wallet DAML SDK provides a complete toolkit for building production-ready wallet applications. It implements the Splice Token Standard with full support for Amulet operations (mint, transfer, burn, lock/unlock), multi-party authorization, external party onboarding, and cryptographic key management.

## Features

### Core Operations
- **Token Standard** - Mint, transfer, burn, lock/unlock Amulets
- **Balance Management** - Real-time balance tracking with UTXO management
- **Multi-party Authorization** - Complex ActAs/ReadAs party configurations
- **Transfer Instructions** - Pending transfer management and execution
- **External Party Support** - External party allocation and onboarding
- **Featured App Rights** - Self-grant and lookup for fee reduction

### Security & Auth
- **OAuth Support** - OAuth2 authentication with token refresh
- **JWT Validation** - Token parsing and validation
- **Cryptographic Operations** - Ed25519 key generation, signing, verification
- **Bearer Token Auth** - Automatic token injection via gRPC interceptors

### Testing
- **Docker Test Containers** - Canton sandbox with automatic lifecycle
- **Integration Tests** - Comprehensive mint and transfer test suite
- **Automatic Setup** - DAR deployment and contract initialization

## Quick Start

### Installation

```bash
go get github.com/noders-team/go-wallet-daml
```

### Basic Usage

```go
import (
    "context"
    "github.com/noders-team/go-wallet-daml/pkg/auth"
    "github.com/noders-team/go-wallet-daml/pkg/sdk"
    "github.com/shopspring/decimal"
)

authProvider := auth.NewUnsafeAuthTokenProvider()

walletSDK, err := sdk.NewWalletSDK(
    "user-123",
    "localhost:6865",
    authProvider,
    false,
)

balance, _ := walletSDK.TokenStandard().GetBalance(ctx)
holdings, _ := walletSDK.TokenStandard().ListHoldingUtxos(ctx, true, 100)
```

## SDK Modules

### Core Components (`pkg/`)

- **`pkg/sdk/`** - High-level wallet SDK entry point
- **`pkg/controller/`** - Core controllers
  - **TokenStandardController** - Token operations (mint, transfer, burn, lock, UTXO management)
  - **LedgerController** - Ledger state management and party operations
  - **ValidatorController** - Validator rights and operations
- **`pkg/auth/`** - Authentication (OAuth, JWT, unsafe mode)
- **`pkg/crypto/`** - Cryptographic utilities (Ed25519, signing, hashing)
- **`pkg/model/`** - Data models and type definitions
- **`pkg/testutil/`** - Testing utilities and Docker container management
- **`pkg/wrapper/`** - HTTP wrappers and scan proxy integration

## Usage Examples

### Transfer Operations

```go
ctx := context.Background()
walletSDK, _ := sdk.NewWalletSDK("alice", "localhost:6865", authProvider, false)

senderParty := model.PartyID("alice::1220...")
receiverParty := model.PartyID("bob::1220...")
amount := decimal.NewFromInt(100)

holdings, _ := walletSDK.TokenStandard().ListHoldingUtxos(ctx, false, 10)
var inputUtxos []string
for _, h := range holdings {
    inputUtxos = append(inputUtxos, h.ContractID)
}

result, _ := walletSDK.TokenStandard().CreateTransfer(
    ctx,
    senderParty,
    receiverParty,
    amount,
    "Amulet",
    "dso::1220...",
    inputUtxos,
    "Payment",
)
```

### Mint (Tap) Operations

```go
receiverParty := model.PartyID("alice::1220...")
amount := decimal.NewFromInt(1000)

result, _ := walletSDK.TokenStandard().CreateAndSubmitTapInternal(
    ctx,
    receiverParty,
    amount,
    "Amulet",
    "dso::1220...",
)
```

### External Party Allocation

```go
import "crypto/ed25519"

publicKey, privateKey, _ := ed25519.GenerateKey(nil)

partyID, _ := walletSDK.Ledger().AllocateExternalParty(
    ctx,
    "external-wallet",
    publicKey,
    privateKey,
    []string{"dso::1220..."},
    "mysynchronizer::1220...",
)
```

## Key Concepts

### Decimal Conversion
DAML NUMERIC requires scaling by 10^10. Always use `decimal.Decimal`:

```go
amount := decimal.NewFromInt(1000)
```

### Multi-party Authorization
Include all relevant parties in ActAs:

```go
ActAs: []string{dsoParty, senderParty, receiverParty}
```

### Balance Retrieval
Use `ListHoldingUtxos()` for accurate balance:

```go
holdings, _ := tokenStandard.ListHoldingUtxos(ctx, true, 100)
totalBalance := decimal.Zero
for _, h := range holdings {
    totalBalance = totalBalance.Add(h.Amount)
}
```

## Development

### Build & Test

```bash
go build

go test ./...

go test -v ./pkg/sdk -run TestExternalPartyWalletWithMintAndTransfer

go test -coverprofile=coverage.out ./...
```

### Prerequisites
- Go 1.23+
- Docker (for integration tests)
- Canton blockchain (for production)

## Supported Operations

| Operation | Description | Requires DSO |
|-----------|-------------|--------------|
| `CreateTap` | Mint new Amulets | Yes |
| `CreateTransfer` | Transfer Amulets | Yes |
| `ListHoldingUtxos` | List holdings | No |
| `GetBalance` | Get balance | No |
| `MergeHoldingUtxos` | Consolidate UTXOs | Yes |
| `AllocateExternalParty` | Create external party | Yes |
| `SelfGrantFeatureAppRights` | Grant app rights | No |

## Troubleshooting

**CONTRACT_NOT_FOUND**: Include all parties in ActAs list

**Balance Returns Zero**: Use `ListHoldingUtxos()` instead of `GetBalance()`

**Test Timing Errors**: Ensure `round0Duration > tickDuration`

## DAML Ecosystem

- **[DAML](https://daml.com/)** - Digital Asset Modeling Language
- **[Canton](https://github.com/digital-asset/canton)** - DAML ledger implementation
- **[Splice](https://www.splice.global/)** - Canton Coin ecosystem
- **[go-daml](https://github.com/noders-team/go-daml)** - Go SDK for DAML Ledger API

## Contributing

1. Fork the repository
2. Create feature branch: `git checkout -b feature/name`
3. Make changes and run tests
4. Commit: `git commit -m 'Add feature'`
5. Push and open Pull Request

## Support

- Open an issue on GitHub
- Check examples and tests for usage patterns
