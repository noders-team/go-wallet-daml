package model

import "errors"

var (
	ErrNotConnected      = errors.New("not connected to network")
	ErrNotAuthenticated  = errors.New("not authenticated")
	ErrInvalidCommand    = errors.New("invalid command format")
	ErrTransactionFailed = errors.New("transaction execution failed")
	ErrSignatureRequired = errors.New("signature required for execution")
	ErrDARNotFound       = errors.New("DAR not found")
	ErrNoUserLedger      = errors.New("user ledger not initialized")
	ErrNoPartyID         = errors.New("party ID not set")
)
