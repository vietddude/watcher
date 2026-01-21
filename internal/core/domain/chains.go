package domain

import "fmt"

type ChainID string

func (c ChainID) String() string {
	return string(c)
}

const (
	// Chain IDs
	EthereumMainnet ChainID = "1"
	PolygonMainnet  ChainID = "137"
	SuiTestnet      ChainID = "784"
)

const (
	// Internal Chain Codes
	EthereumCode = "ETHEREUM_MAINNET"
	PolygonCode  = "POLYGON_MAINNET"
	SuiTestCode  = "SUI_TEST"
)

var chainIDToCode = map[ChainID]string{
	EthereumMainnet: EthereumCode,
	PolygonMainnet:  PolygonCode,
	SuiTestnet:      SuiTestCode,
}

var chainCodeToID = map[string]ChainID{
	EthereumCode: EthereumMainnet,
	PolygonCode:  PolygonMainnet,
	SuiTestCode:  SuiTestnet,
}

func ChainNameFromID(id ChainID) (string, error) {
	code, ok := chainIDToCode[id]
	if !ok {
		return "", fmt.Errorf("unknown chain id: %s", id)
	}
	return code, nil
}

func ChainIDFromCode(code string) (ChainID, error) {
	id, ok := chainCodeToID[code]
	if !ok {
		return "", fmt.Errorf("unknown chain code: %s", code)
	}
	return id, nil
}
