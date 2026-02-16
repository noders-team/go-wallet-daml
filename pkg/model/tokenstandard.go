package model

import (
	damlModel "github.com/noders-team/go-daml/pkg/model"
	"github.com/shopspring/decimal"
)

type HoldingUTXO struct {
	ContractID       string
	Amount           decimal.Decimal
	InstrumentID     string
	InstrumentAdmin  string
	Owner            string
	Lock             map[string]interface{}
	CreatedEventBlob []byte
}

type TransferInstruction struct {
	ContractID       string
	Sender           string
	Receiver         string
	Amount           decimal.Decimal
	Memo             string
	CreatedEventBlob []byte
}

type MergeUtxosResult struct {
	Commands           []*damlModel.Command
	DisclosedContracts []*damlModel.DisclosedContract
}

type AllocationInstruction struct {
	ContractID       string
	Provider         string
	Specification    map[string]interface{}
	CreatedEventBlob []byte
}

type AllocationRequest struct {
	ContractID       string
	Requester        string
	Specification    map[string]interface{}
	CreatedEventBlob []byte
}

type Allocation struct {
	ContractID       string
	Provider         string
	Receiver         string
	Amount           decimal.Decimal
	CreatedEventBlob []byte
}
