package dapp

type StatusEvent struct {
	Kernel             *KernelInfo  `json:"kernel"`
	IsConnected        bool         `json:"isConnected"`
	IsNetworkConnected bool         `json:"isNetworkConnected"`
	NetworkReason      string       `json:"networkReason,omitempty"`
	Network            *NetworkInfo `json:"network,omitempty"`
	Session            *SessionInfo `json:"session,omitempty"`
}

type KernelInfo struct {
	ID         string `json:"id"`
	ClientType string `json:"clientType"`
	URL        string `json:"url,omitempty"`
	UserURL    string `json:"userUrl,omitempty"`
}

type NetworkInfo struct {
	NetworkID string         `json:"networkId"`
	LedgerApi *LedgerApiInfo `json:"ledgerApi,omitempty"`
}

type LedgerApiInfo struct {
	BaseURL string `json:"baseUrl"`
}

type SessionInfo struct {
	AccessToken string `json:"accessToken"`
	UserID      string `json:"userId"`
}

type JsPrepareSubmissionRequest struct {
	CommandID                     string               `json:"commandId,omitempty"`
	Commands                      interface{}          `json:"commands"`
	ActAs                         []string             `json:"actAs,omitempty"`
	ReadAs                        []string             `json:"readAs,omitempty"`
	DisclosedContracts            []*DisclosedContract `json:"disclosedContracts,omitempty"`
	SynchronizerID                string               `json:"synchronizerId,omitempty"`
	PackageIDSelectionPreference  []string             `json:"packageIdSelectionPreference,omitempty"`
}

type DisclosedContract struct {
	TemplateID       string `json:"templateId,omitempty"`
	ContractID       string `json:"contractId,omitempty"`
	CreatedEventBlob string `json:"createdEventBlob"`
	SynchronizerID   string `json:"synchronizerId,omitempty"`
}

type JsPrepareSubmissionResponse struct {
	PreparedTransaction     string `json:"preparedTransaction"`
	PreparedTransactionHash string `json:"preparedTransactionHash"`
}

type LedgerApiRequest struct {
	RequestMethod string `json:"requestMethod"`
	Resource      string `json:"resource"`
	Body          string `json:"body,omitempty"`
}

type LedgerApiResult struct {
	Response string `json:"response"`
}

type Wallet struct {
	Address         string `json:"address"`
	NetworkID       string `json:"networkId"`
	SigningProvider string `json:"signingProvider"`
}

type TxChangedPendingEvent struct {
	Status    string `json:"status"`
	CommandID string `json:"commandId"`
}

type TxChangedSignedEvent struct {
	Status    string                  `json:"status"`
	CommandID string                  `json:"commandId"`
	Payload   *TxChangedSignedPayload `json:"payload"`
}

type TxChangedSignedPayload struct {
	Signature string `json:"signature"`
	SignedBy  string `json:"signedBy"`
	Party     string `json:"party"`
}

type TxChangedExecutedEvent struct {
	Status    string                    `json:"status"`
	CommandID string                    `json:"commandId"`
	Payload   *TxChangedExecutedPayload `json:"payload"`
}

type TxChangedExecutedPayload struct {
	UpdateID         string `json:"updateId"`
	CompletionOffset int64  `json:"completionOffset"`
}

type TxChangedFailedEvent struct {
	Status    string `json:"status"`
	CommandID string `json:"commandId"`
}
