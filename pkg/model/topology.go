package model

type GenerateTransactionResponse struct {
	PartyID                string
	Namespace              string
	PublicKeyFingerprint   string
	MultiHash              string
	TopologyTransactions   []string
}

type TopologyTransaction struct {
	Transaction string
}

type Signature struct {
	Format               string
	Signature            string
	SignedBy             string
	SigningAlgorithmSpec string
}

type AllocateExternalPartyResponse struct {
	PartyID string
}

type ParticipantEndpointConfig struct {
	GRPCAddress         string
	HTTPBaseURL         string
	AccessTokenProvider interface{}
}
