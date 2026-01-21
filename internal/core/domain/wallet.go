package domain

// WalletAddress represents a monitored wallet address
type WalletAddress struct {
	Address  string
	Type     NetworkType
	Standard AddressStandard
}

type NetworkType string

const (
	NetworkTypeEVM     NetworkType = "evm"
	NetworkTypeBitcoin NetworkType = "bitcoin"
	NetworkTypeSolana  NetworkType = "solana"
)

type AddressStandard string

const (
	AddressStandardEOA     AddressStandard = "eoa"
	AddressStandardERC20   AddressStandard = "erc20"
	AddressStandardERC721  AddressStandard = "erc721"
	AddressStandardERC1155 AddressStandard = "erc1155"
)
