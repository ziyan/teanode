package trie

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
)

type ipVersion string

// Helper constants.
const (
	ipv4Uint32Count = 1
	ipv6Uint32Count = 4

	bitsPerUint32 = 32
	bytePerUint32 = 4

	ipv4 ipVersion = "ipv4"
	ipv6 ipVersion = "ipv6"
)

// networkNumber represents an IP address using uint32 as internal storage.
// IPv4 usings 1 uint32, while IPv6 uses 4 uint32.
type networkNumber []uint32

// newNetworkNumber returns a equivalent networkNumber to given IP address,
// return nil if ip is neither IPv4 nor IPv6.
func newNetworkNumber(ip net.IP) networkNumber {
	if ip == nil {
		return nil
	}
	coercedIp := ip.To4()
	parts := 1
	if coercedIp == nil {
		coercedIp = ip.To16()
		parts = 4
	}
	if coercedIp == nil {
		return nil
	}
	nn := make(networkNumber, parts)
	for i := 0; i < parts; i++ {
		index := i * net.IPv4len
		nn[i] = binary.BigEndian.Uint32(coercedIp[index : index+net.IPv4len])
	}
	return nn
}

// ToV4 returns ip address if ip is IPv4, returns nil otherwise.
func (self networkNumber) ToV4() networkNumber {
	if len(self) != ipv4Uint32Count {
		return nil
	}
	return self
}

// ToV6 returns ip address if ip is IPv6, returns nil otherwise.
func (self networkNumber) ToV6() networkNumber {
	if len(self) != ipv6Uint32Count {
		return nil
	}
	return self
}

// ToIP returns equivalent net.IP.
func (self networkNumber) ToIP() net.IP {
	ip := make(net.IP, len(self)*bytePerUint32)
	for i := 0; i < len(self); i++ {
		index := i * net.IPv4len
		binary.BigEndian.PutUint32(ip[index:index+net.IPv4len], self[i])
	}
	if len(ip) == net.IPv4len {
		ip = net.IPv4(ip[0], ip[1], ip[2], ip[3])
	}
	return ip
}

// Equal is the equality test for 2 network numbers.
func (self networkNumber) Equal(number networkNumber) bool {
	if len(self) != len(number) {
		return false
	}
	if self[0] != number[0] {
		return false
	}
	if len(self) == ipv6Uint32Count {
		return self[1] == number[1] && self[2] == number[2] && self[3] == number[3]
	}
	return true
}

// Next returns the next logical network number.
func (self networkNumber) Next() networkNumber {
	newIp := make(networkNumber, len(self))
	copy(newIp, self)
	for i := len(newIp) - 1; i >= 0; i-- {
		newIp[i]++
		if newIp[i] > 0 {
			break
		}
	}
	return newIp
}

// Previous returns the previous logical network number.
func (self networkNumber) Previous() networkNumber {
	newIp := make(networkNumber, len(self))
	copy(newIp, self)
	for i := len(newIp) - 1; i >= 0; i-- {
		newIp[i]--
		if newIp[i] < math.MaxUint32 {
			break
		}
	}
	return newIp
}

// Bit returns uint32 representing the bit value at given position, e.g.,
// "128.0.0.0" has bit value of 1 at position 31, and 0 for positions 30 to 0.
func (self networkNumber) Bit(position uint) (uint32, error) {
	if int(position) > len(self)*bitsPerUint32-1 {
		return 0, fmt.Errorf("trie: bit position not valid")
	}
	index := len(self) - 1 - int(position/bitsPerUint32)
	// Mod 31 to get array index.
	rightShift := position & (bitsPerUint32 - 1)
	return (self[index] >> rightShift) & 1, nil
}

// LeastCommonBitPosition returns the smallest position of the preceding common
// bits of the 2 network numbers, and returns an error fmt.Errorf("trie: no greatest common bit")
// if the two network number diverges from the first bit.
// e.g., if the network number diverges after the 1st bit, it returns 131 for
// IPv6 and 31 for IPv4 .
func (self networkNumber) LeastCommonBitPosition(number networkNumber) (uint, error) {
	if len(self) != len(number) {
		return 0, fmt.Errorf("trie: network input version mismatch")
	}
	for i := 0; i < len(self); i++ {
		mask := uint32(1) << 31
		pos := uint(31)
		for ; mask > 0; mask >>= 1 {
			if self[i]&mask != number[i]&mask {
				if i == 0 && pos == 31 {
					return 0, fmt.Errorf("trie: no greatest common bit")
				}
				return (pos + 1) + uint(bitsPerUint32)*uint(len(self)-i-1), nil
			}
			pos--
		}
	}
	return 0, nil
}

// network represents a block of network numbers, also known as CIDR.
type network struct {
	net.IPNet
	number networkNumber
	mask   networkNumberMask
}

// newNetwork returns network built using given net.IPNet.
func newNetwork(ipNet net.IPNet) network {
	return network{
		IPNet:  ipNet,
		number: newNetworkNumber(ipNet.IP),
		mask:   networkNumberMask(newNetworkNumber(net.IP(ipNet.Mask))),
	}
}

// Masked returns a new network conforming to new mask.
func (self network) Masked(ones int) network {
	mask := net.CIDRMask(ones, len(self.number)*bitsPerUint32)
	return newNetwork(net.IPNet{
		IP:   self.IP.Mask(mask),
		Mask: mask,
	})
}

// Contains returns true if networkNumber is in range of network, false
// otherwise.
func (self network) Contains(number networkNumber) bool {
	if len(self.mask) != len(number) {
		return false
	}
	if number[0]&self.mask[0] != self.number[0] {
		return false
	}
	if len(number) == ipv6Uint32Count {
		return number[1]&self.mask[1] == self.number[1] && number[2]&self.mask[2] == self.number[2] && number[3]&self.mask[3] == self.number[3]
	}
	return true
}

// Contains returns true if network covers o, false otherwise.
func (self network) Covers(network network) bool {
	if len(self.number) != len(network.number) {
		return false
	}
	nMaskSize, _ := self.Mask.Size()
	oMaskSize, _ := network.Mask.Size()
	return self.Contains(network.number) && nMaskSize <= oMaskSize
}

// LeastCommonBitPosition returns the smallest position of the preceding common
// bits of the 2 networks, and returns an error fmt.Errorf("trie: no greatest common bit")
// if the two network number diverges from the first bit.
func (self network) LeastCommonBitPosition(network network) (uint, error) {
	maskSize, _ := self.Mask.Size()
	if networkMaskSize, _ := network.Mask.Size(); networkMaskSize < maskSize {
		maskSize = networkMaskSize
	}
	maskPosition := len(network.number)*bitsPerUint32 - maskSize
	lcb, err := self.number.LeastCommonBitPosition(network.number)
	if err != nil {
		return 0, err
	}
	return uint(math.Max(float64(maskPosition), float64(lcb))), nil
}

// Equal is the equality test for 2 networks.
func (self network) Equal(network network) bool {
	return self.String() == network.String()
}

func (self network) String() string {
	return self.IPNet.String()
}

// networkNumberMask is an IP address.
type networkNumberMask networkNumber

// Mask returns a new masked networkNumber from giveself networkNumber.
func (self networkNumberMask) Mask(number networkNumber) (networkNumber, error) {
	if len(self) != len(number) {
		return nil, fmt.Errorf("trie: network input version mismatch")
	}
	result := make(networkNumber, len(self))
	result[0] = self[0] & number[0]
	if len(self) == ipv6Uint32Count {
		result[1] = self[1] & number[1]
		result[2] = self[2] & number[2]
		result[3] = self[3] & number[3]
	}
	return result, nil
}

// NextIP returns the next sequential ip.
func NextIP(ip net.IP) net.IP {
	return newNetworkNumber(ip).Next().ToIP()
}

// PreviousIP returns the previous sequential ip.
func PreviousIP(ip net.IP) net.IP {
	return newNetworkNumber(ip).Previous().ToIP()
}
