package domain

type ChainID string
type ChainName string

const (
	// Chain IDs
	ChainIDEthereum ChainID = "1"
	ChainIDPolygon  ChainID = "137"
	ChainIDSuiTest  ChainID = "784"

	// Chain Names (Internal Codes)
	ChainNameEthereum ChainName = "ETHEREUM_MAINNET"
	ChainNamePolygon  ChainName = "POLYGON_MAINNET"
	ChainNameSuiTest  ChainName = "SUI_TEST"
)

// ChainIDToName maps ChainID to its human-readable InternalCode/Name.
var ChainIDToName = map[ChainID]ChainName{
	ChainIDEthereum: ChainNameEthereum,
	ChainIDPolygon:  ChainNamePolygon,
	ChainIDSuiTest:  ChainNameSuiTest,
}

// ChainNameToID maps Chain Name to its ID.
var ChainNameToID = map[ChainName]ChainID{
	ChainNameEthereum: ChainIDEthereum,
	ChainNamePolygon:  ChainIDPolygon,
	ChainNameSuiTest:  ChainIDSuiTest,
}
