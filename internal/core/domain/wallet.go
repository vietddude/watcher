package domain

import (
	"time"
)

// WalletAddress represents a monitored wallet address
type WalletAddress struct {
	ID        uint64
	Address   string
	Type      NetworkType
	Standard  AddressStandard
	CreatedAt time.Time
	UpdatedAt time.Time
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
