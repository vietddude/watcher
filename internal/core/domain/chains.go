package domain

const (
	// Chain IDs
	ChainIDEthereum = "1"
	ChainIDPolygon  = "137"
	ChainIDSuiTest  = "784"

	// Chain Names (Internal Codes)
	ChainNameEthereum = "ETHEREUM_MAINNET"
	ChainNamePolygon  = "POLYGON_MAINNET"
	ChainNameSuiTest  = "SUI_TEST"
)

// ChainIDToName maps ChainID to its human-readable InternalCode/Name.
var ChainIDToName = map[string]string{
	ChainIDEthereum: ChainNameEthereum,
	ChainIDPolygon:  ChainNamePolygon,
	ChainIDSuiTest:  ChainNameSuiTest,
}

// ChainNameToID maps Chain Name to its ID.
var ChainNameToID = map[string]string{
	ChainNameEthereum: ChainIDEthereum,
	ChainNamePolygon:  ChainIDPolygon,
	ChainNameSuiTest:  ChainIDSuiTest,
}
