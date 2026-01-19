package domain

// WalletAddress represents a monitored wallet address
type WalletAddress struct {
	ID        uint64          `json:"id"`
	Address   string          `json:"address"`
	Type      NetworkType     `json:"network_type"`
	Standard  AddressStandard `json:"standard"`
	CreatedAt uint64          `json:"created_at"`
	UpdatedAt uint64          `json:"updated_at"`
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
