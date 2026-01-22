package domain

import "fmt"

type ChainID string

func (c ChainID) String() string {
	return string(c)
}

type ChainType string

const (
	ChainTypeEVM     ChainType = "evm"
	ChainTypeSui     ChainType = "sui"
	ChainTypeBitcoin ChainType = "bitcoin"
	ChainTypeTron    ChainType = "tron"
)

const (
	EthereumMainnet ChainID = "ETHEREUM_MAINNET"
	PolygonMainnet  ChainID = "POLYGON_MAINNET"
	SuiTestnet      ChainID = "SUI_TESTNET"
	SuiMainnet      ChainID = "SUI_MAINNET"
	BitcoinMainnet  ChainID = "BITCOIN_MAINNET"
)

// ChainNameFromID now just returns the string representation of the ID
// since we use descriptive internal codes as IDs.
func ChainNameFromID(id ChainID) (string, error) {
	if id == "" {
		return "", fmt.Errorf("empty chain id")
	}
	return string(id), nil
}

// ChainIDFromCode is now an identity function since code == ID.
func ChainIDFromCode(code string) (ChainID, error) {
	if code == "" {
		return "", fmt.Errorf("empty chain code")
	}
	return ChainID(code), nil
}
