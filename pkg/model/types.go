package model

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

type PartyID string

type AuthContext struct {
	UserID      string
	AccessToken string
}

type WalletError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

type ErrorCode int

const (
	ErrCodeAuth ErrorCode = iota + 1
	ErrCodeLedger
	ErrCodeValidation
	ErrCodeNetwork
	ErrCodeTimeout
	ErrCodeNotFound
)

func (e *WalletError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *WalletError) Unwrap() error {
	return e.Cause
}

func NewWalletError(code ErrorCode, message string, cause error) *WalletError {
	return &WalletError{
		Code:    code,
		Message: message,
		Cause:   cause,
	}
}

type PrepareSubmissionResponse struct {
	PreparedTransaction     []byte
	PreparedTransactionHash string
}

type TransferResponse struct {
	SubmissionID string
	Amount       decimal.Decimal
	Receiver     PartyID
}

type LockResponse struct {
	ContractID string
	Amount     decimal.Decimal
	ExpiresAt  time.Time
}

type TransferPreapproval struct {
	ReceiverID PartyID
	ExpiresAt  time.Time
	DSO        PartyID
}

type OpenMiningRound struct {
	RoundID   string
	StartTime time.Time
	EndTime   time.Time
}

type AmuletRules struct {
	DSOParty PartyID
	Rules    map[string]interface{}
}

type ListSynchronizersResponse struct {
	ConnectedSynchronizers []*SynchronizerInfo
}

type SynchronizerInfo struct {
	SynchronizerID string
	Permission     ParticipantPermission
}

type ParticipantPermission int32

const (
	ParticipantPermissionSubmission ParticipantPermission = iota
	ParticipantPermissionConfirmation
	ParticipantPermissionObservation
)

type CompletionValue struct {
	CommandID     string
	Status        string
	UpdateID      string
	TransactionID string
	SubmissionID  string
	CompletedAt   *time.Time
	Offset        int64
}

type ActiveContract struct {
	ContractID       string
	TemplateID       string
	CreateArguments  map[string]interface{}
	Signatories      []string
	Observers        []string
	CreatedAt        *time.Time
	ContractKey      interface{}
	WitnessParties   []string
	CreatedEventBlob []byte
}

type TransactionTree struct {
	UpdateID    string
	CommandID   string
	WorkflowID  string
	EffectiveAt *time.Time
	Events      []*Event
	Offset      int64
}

type Event struct {
	Created   *CreatedEvent
	Archived  *ArchivedEvent
	Exercised *ExercisedEvent
}

type CreatedEvent struct {
	Offset           int64
	NodeID           int32
	ContractID       string
	TemplateID       string
	ContractKey      interface{}
	CreateArguments  interface{}
	CreatedEventBlob []byte
	WitnessParties   []string
	Signatories      []string
	Observers        []string
	CreatedAt        *time.Time
	PackageName      string
}

type ArchivedEvent struct {
	Offset         int64
	NodeID         int32
	ContractID     string
	TemplateID     string
	WitnessParties []string
	PackageName    string
}

type ExercisedEvent struct {
	Offset          int64
	NodeID          int32
	ContractID      string
	TemplateID      string
	InterfaceID     string
	Choice          string
	ChoiceArgument  interface{}
	ActingParties   []string
	Consuming       bool
	WitnessParties  []string
	ExerciseResult  interface{}
	PackageName     string
}

type TransactionFilter struct {
	FiltersByParty map[string]*Filters
}

type Filters struct {
	Inclusive *InclusiveFilters
}

type InclusiveFilters struct{
	TemplateFilters  []*TemplateFilter
	InterfaceFilters []*InterfaceFilter
}

type TemplateFilter struct {
	TemplateID              string
	IncludeCreatedEventBlob bool
}

type InterfaceFilter struct {
	InterfaceID             string
	IncludeInterfaceView    bool
	IncludeCreatedEventBlob bool
}

type DisclosedContract struct {
	TemplateID       string
	ContractID       string
	CreatedEventBlob []byte
	SynchronizerID   string
}
