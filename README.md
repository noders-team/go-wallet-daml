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
    "github.com/noders-team/go-wallet-daml/pkg/controller"
    "github.com/noders-team/go-wallet-daml/pkg/model"
    "github.com/noders-team/go-wallet-daml/pkg/sdk"
    "github.com/shopspring/decimal"
)

walletSDK := sdk.NewWalletSDK()
walletSDK.Configure(sdk.Config{
    AuthFactory: func() auth.AuthController {
        return auth.NewMockAuthController("user-123")
    },
    LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
        return controller.NewLedgerController(userID, "localhost:6865", "", provider, isAdmin)
    },
    TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
        return controller.NewTokenStandardController(userID, "localhost:6865", provider, isAdmin)
    },
})

err := walletSDK.Connect(ctx)

partyID := model.PartyID("alice::1220...")
synchronizerID := model.PartyID("mysynchronizer::1220...")
walletSDK.SetPartyID(ctx, partyID, &synchronizerID)

balance, _ := walletSDK.TokenStandard().GetBalance(ctx)
holdings, _ := walletSDK.TokenStandard().ListHoldingUtxos(ctx, true, 100)
```

## SDK Architecture

### WalletSDK Methods

- **`NewWalletSDK()`** - Creates new SDK instance
- **`Configure(config Config)`** - Configures SDK with factory functions
- **`Connect(ctx)`** - Connects with user token and initializes user controllers
- **`ConnectAdmin(ctx)`** - Connects with admin token and initializes admin controllers
- **`SetPartyID(ctx, partyID, synchronizerID)`** - Sets party and synchronizer for all controllers

### Controller Accessors

- **`TokenStandard()`** - Returns TokenStandardController for token operations
- **`UserLedger()`** - Returns user LedgerController
- **`AdminLedger()`** - Returns admin LedgerController (requires ConnectAdmin)
- **`Validator()`** - Returns ValidatorController
- **`Auth()`** - Returns AuthController
- **`AuthTokenProvider()`** - Returns AuthTokenProvider

### Core Components (`pkg/`)

- **`pkg/sdk/`** - High-level wallet SDK entry point with factory pattern
- **`pkg/controller/`** - Core controllers
  - **TokenStandardController** - Token operations (mint, transfer, burn, lock, UTXO management)
  - **LedgerController** - Ledger state management and party operations
  - **ValidatorController** - Validator rights and operations
- **`pkg/auth/`** - Authentication (OAuth, JWT, mock mode)
- **`pkg/crypto/`** - Cryptographic utilities (Ed25519, signing, hashing)
- **`pkg/model/`** - Data models and type definitions
- **`pkg/testutil/`** - Testing utilities and Docker container management
- **`pkg/wrapper/`** - HTTP wrappers and scan proxy integration

## Usage Examples

### SDK Initialization

```go
ctx := context.Background()
grpcAddr := "localhost:6865"
scanProxyURL := "http://localhost:5012"

walletSDK := sdk.NewWalletSDK()
walletSDK.Configure(sdk.Config{
    AuthFactory: func() auth.AuthController {
        return auth.NewMockAuthController("alice")
    },
    LedgerFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.LedgerController, error) {
        return controller.NewLedgerController(userID, grpcAddr, scanProxyURL, provider, isAdmin)
    },
    TokenStandardFactory: func(userID string, provider *auth.AuthTokenProvider, isAdmin bool) (*controller.TokenStandardController, error) {
        return controller.NewTokenStandardController(userID, grpcAddr, provider, isAdmin)
    },
    ValidatorFactory: func(userID string, provider *auth.AuthTokenProvider) (*controller.ValidatorController, error) {
        return controller.NewValidatorController(userID, grpcAddr, scanProxyURL, provider)
    },
})

err := walletSDK.Connect(ctx)

partyID := model.PartyID("alice::1220...")
synchronizerID := model.PartyID("mysynchronizer::1220...")
walletSDK.SetPartyID(ctx, partyID, &synchronizerID)
```

### Transfer Operations

```go
senderParty := model.PartyID("alice::1220...")
receiverParty := model.PartyID("bob::1220...")
amount := decimal.NewFromFloat(100.0)

walletSDK.SetPartyID(ctx, senderParty, nil)

holdings, _ := walletSDK.TokenStandard().ListHoldingUtxos(ctx, false, 100)
var inputUtxos []string
for _, h := range holdings {
    inputUtxos = append(inputUtxos, h.ContractID)
}

result, _ := walletSDK.TokenStandard().CreateTransfer(
    ctx,
    senderParty,
    receiverParty,
    amount,
    holdings[0].InstrumentID,
    holdings[0].InstrumentAdmin,
    inputUtxos,
    "payment-description",
)
```

### Mint (Tap) Operations

```go
dsoParty := model.PartyID("dso::1220...")
receiverParty := model.PartyID("alice::1220...")
amount := decimal.NewFromFloat(1000.0)

walletSDK.SetPartyID(ctx, dsoParty, nil)

result, _ := walletSDK.TokenStandard().CreateAndSubmitTapInternal(
    ctx,
    receiverParty,
    amount,
    "",
    string(dsoParty),
)
```

### Admin Operations

```go
err := walletSDK.ConnectAdmin(ctx)

adminLedger := walletSDK.AdminLedger()
```

## Key Concepts

### Party and Synchronizer Management
Set party and synchronizer before performing operations:

```go
partyID := model.PartyID("alice::1220...")
synchronizerID := model.PartyID("mysynchronizer::1220...")

walletSDK.SetPartyID(ctx, partyID, &synchronizerID)

walletSDK.SetPartyID(ctx, partyID, nil)
```

### Decimal Conversion
Always use `decimal.Decimal` for amounts:

```go
amount := decimal.NewFromFloat(100.0)
amount := decimal.NewFromInt(1000)
```

### Multi-party Authorization
Include all relevant parties in ActAs when submitting commands:

```go
ActAs: []string{dsoParty, senderParty, receiverParty}
```

### Balance Retrieval
Use `ListHoldingUtxos()` for accurate balance calculation:

```go
holdings, _ := walletSDK.TokenStandard().ListHoldingUtxos(ctx, true, 100)
totalBalance := decimal.Zero
for _, h := range holdings {
    totalBalance = totalBalance.Add(h.Amount)
}
```

## Development

### Build & Test

```bash
make deps

make test

make test-coverage

go test -v ./pkg/sdk -run TestExternalPartyWalletWithMintAndTransfer
```

Or using standard Go commands:

```bash
go mod tidy

go test ./...

go test -v -coverprofile=coverage.out ./...
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
