package model

type GenerateTransactionResponse struct {
	PartyID              string
	Namespace            string
	PublicKeyFingerprint string
	MultiHash            string
	TopologyTransactions []string
}

type AllocateExternalPartyResponse struct {
	PartyID string
}

type ParticipantEndpointConfig struct {
	GRPCAddress         string
	HTTPBaseURL         string
	AccessTokenProvider interface{}
}
