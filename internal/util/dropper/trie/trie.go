// Package trie is a path-compressed (PC) trie implementation of the
// ranger interface inspired by this blog post:
// https://vincent.bernat.im/en/blog/2017-ipv4-route-lookup-linux
//
// CIDR blocks are stored using a prefix tree structure where each node has its
// parent as prefix, and the path from the root node represents current CIDR
// block.
//
// For IPv4, the trie structure guarantees max depth of 32 as IPv4 addresses are
// 32 bits long and each bit represents a prefix tree starting at that bit. This
// property also guarantees constant lookup time in Big-O notation.
//
// Path compression compresses a string of node with only 1 child into a single
// node, decrease the amount of lookups necessary during containment tests.
//
// Level compression dictates the amount of direct children of a node by
// allowing it to handle multiple bits in the path.  The heuristic (based on
// children population) to decide when the compression and decompression happens
// is outlined in the prior linked blog, and will be experimented with in more
// depth in this project in the future.
//
// Note: Can not insert both IPv4 and IPv6 network addresses into the same
// prefix trie, use versionedRanger wrapper instead.
//
// TODO: Implement level-compressed component of the LPC trie.
package trie

import (
	"fmt"
	"net"
	"strings"

	"github.com/ziyan/teanode/internal/util/deferutil"
)

type Entry interface {
	Network() net.IPNet
}

type Trie interface {
	// Insert inserts a Entry into prefix trie.
	Insert(entry Entry) error

	// Remove removes Entry identified by given network from trie.
	Remove(network net.IPNet) (Entry, error)

	// Contains returns boolean indicating whether given ip is contained in any
	// of the inserted networks.
	Contains(ip net.IP) (bool, error)

	// ContainingNetworks returns the list of Entry(s) the given ip is
	// contained in in ascending prefix order.
	ContainingNetworks(ip net.IP) ([]Entry, error)

	// CoveredNetworks returns the list of Entry(s) the given ipnet
	// covers. That is, the networks that are completely subsumed by the
	// specified network.
	CoveredNetworks(network net.IPNet) ([]Entry, error)

	// Len returns number of networks in ranger.
	Len() int

	// String returns string representation of trie, mainly for visualization and
	// debugging.
	String() string

	// WalkDepth walks the trie in depth order, for unit testing.
	WalkDepth() <-chan Entry
}

type trie struct {
	parent   *trie
	children []*trie

	bitsSkipped uint
	bitsHandled uint

	network network
	entry   Entry

	size int // This is only maintained in the root trie.
}

func New4() Trie {
	return newTrie(ipv4)
}

func New6() Trie {
	return newTrie(ipv6)
}

// newTrie creates a new trie.
func newTrie(version ipVersion) *trie {
	_, rootNet, _ := net.ParseCIDR("0.0.0.0/0")
	if version == ipv6 {
		_, rootNet, _ = net.ParseCIDR("0::0/0")
	}
	return &trie{
		children:    make([]*trie, 2),
		bitsSkipped: 0,
		bitsHandled: 1,
		network:     newNetwork(*rootNet),
	}
}

func newPathTrie(network network, bitsSkipped uint) *trie {
	version := ipv4
	if len(network.number) == ipv6Uint32Count {
		version = ipv6
	}
	path := newTrie(version)
	path.bitsSkipped = bitsSkipped
	path.network = network.Masked(int(bitsSkipped))
	return path
}

func newEntryTrie(network network, entry Entry) *trie {
	ones, _ := network.Mask.Size()
	leaf := newPathTrie(network, uint(ones))
	leaf.entry = entry
	return leaf
}

func (self *trie) Insert(entry Entry) error {
	network := entry.Network()
	sizeIncreased, err := self.insert(newNetwork(network), entry)
	if sizeIncreased {
		self.size++
	}
	return err
}

func (self *trie) Remove(network net.IPNet) (Entry, error) {
	entry, err := self.remove(newNetwork(network))
	if entry != nil {
		self.size--
	}
	return entry, err
}

func (self *trie) Contains(ip net.IP) (bool, error) {
	number := newNetworkNumber(ip)
	if number == nil {
		return false, fmt.Errorf("trie: invalid network number input")
	}
	return self.contains(number)
}

func (self *trie) ContainingNetworks(ip net.IP) ([]Entry, error) {
	number := newNetworkNumber(ip)
	if number == nil {
		return nil, fmt.Errorf("trie: invalid network number input")
	}
	return self.containingNetworks(number)
}

func (self *trie) CoveredNetworks(network net.IPNet) ([]Entry, error) {
	net := newNetwork(network)
	return self.coveredNetworks(net)
}

func (self *trie) Len() int {
	return self.size
}

func (self *trie) String() string {
	children := []string{}
	padding := strings.Repeat("| ", self.level()+1)
	for bits, child := range self.children {
		if child == nil {
			continue
		}
		childStr := fmt.Sprintf("\n%s%d--> %s", padding, bits, child.String())
		children = append(children, childStr)
	}
	return fmt.Sprintf("%s (target_pos:%d:has_entry:%t)%s", self.network, self.targetBitPosition(), self.hasEntry(), strings.Join(children, ""))
}

func (self *trie) contains(number networkNumber) (bool, error) {
	if !self.network.Contains(number) {
		return false, nil
	}
	if self.hasEntry() {
		return true, nil
	}
	if self.targetBitPosition() < 0 {
		return false, nil
	}
	bit, err := self.targetBitFromIp(number)
	if err != nil {
		return false, err
	}
	child := self.children[bit]
	if child != nil {
		return child.contains(number)
	}
	return false, nil
}

func (self *trie) containingNetworks(number networkNumber) ([]Entry, error) {
	results := []Entry{}
	if !self.network.Contains(number) {
		return results, nil
	}
	if self.hasEntry() {
		results = []Entry{self.entry}
	}
	if self.targetBitPosition() < 0 {
		return results, nil
	}
	bit, err := self.targetBitFromIp(number)
	if err != nil {
		return nil, err
	}
	child := self.children[bit]
	if child != nil {
		ranges, err := child.containingNetworks(number)
		if err != nil {
			return nil, err
		}
		if len(ranges) > 0 {
			if len(results) > 0 {
				results = append(results, ranges...)
			} else {
				results = ranges
			}
		}
	}
	return results, nil
}

func (self *trie) coveredNetworks(network network) ([]Entry, error) {
	var results []Entry
	if network.Covers(self.network) {
		for entry := range self.WalkDepth() {
			results = append(results, entry)
		}
	} else if self.targetBitPosition() >= 0 {
		bit, err := self.targetBitFromIp(network.number)
		if err != nil {
			return results, err
		}
		child := self.children[bit]
		if child != nil {
			return child.coveredNetworks(network)
		}
	}
	return results, nil
}

func (self *trie) insert(network network, entry Entry) (bool, error) {
	if self.network.Equal(network) {
		sizeIncreased := self.entry == nil
		self.entry = entry
		return sizeIncreased, nil
	}

	bit, err := self.targetBitFromIp(network.number)
	if err != nil {
		return false, err
	}
	existingChild := self.children[bit]

	// No existing child, insert new leaf trie.
	if existingChild == nil {
		self.appendTrie(bit, newEntryTrie(network, entry))
		return true, nil
	}

	// Check whether it is necessary to insert additional path prefix between current trie and existing child,
	// in the case that inserted network diverges on its path to existing child.
	lcb, err := network.LeastCommonBitPosition(existingChild.network)
	if err != nil {
		return false, err
	}
	divergingBitPos := int(lcb) - 1
	if divergingBitPos > existingChild.targetBitPosition() {
		pathPrefix := newPathTrie(network, self.totalNumberOfBits()-lcb)
		err := self.insertPrefix(bit, pathPrefix, existingChild)
		if err != nil {
			return false, err
		}
		// Update new child
		existingChild = pathPrefix
	}
	return existingChild.insert(network, entry)
}

func (self *trie) appendTrie(bit uint32, prefix *trie) {
	self.children[bit] = prefix
	prefix.parent = self
}

func (self *trie) insertPrefix(bit uint32, pathPrefix, child *trie) error {
	// Set parent/child relationship between current trie and inserted pathPrefix
	self.children[bit] = pathPrefix
	pathPrefix.parent = self

	// Set parent/child relationship between inserted pathPrefix and original child
	pathPrefixBit, err := pathPrefix.targetBitFromIp(child.network.number)
	if err != nil {
		return err
	}
	pathPrefix.children[pathPrefixBit] = child
	child.parent = pathPrefix
	return nil
}

func (self *trie) remove(network network) (Entry, error) {
	if self.hasEntry() && self.network.Equal(network) {
		entry := self.entry
		self.entry = nil

		err := self.compressPathIfPossible()
		if err != nil {
			return nil, err
		}
		return entry, nil
	}
	if self.targetBitPosition() < 0 {
		return nil, nil
	}
	bit, err := self.targetBitFromIp(network.number)
	if err != nil {
		return nil, err
	}
	child := self.children[bit]
	if child != nil {
		return child.remove(network)
	}
	return nil, nil
}

func (self *trie) qualifiesForPathCompression() bool {
	// Current prefix trie can be path compressed if it meets all following.
	//		1. records no CIDR entry
	//		2. has single or no child
	//		3. is not root trie
	return !self.hasEntry() && self.childrenCount() <= 1 && self.parent != nil
}

func (self *trie) compressPathIfPossible() error {
	if !self.qualifiesForPathCompression() {
		// Does not qualify to be compressed
		return nil
	}

	// Find lone child.
	var loneChild *trie
	for _, child := range self.children {
		if child != nil {
			loneChild = child
			break
		}
	}

	// Find root of currnt single child lineage.
	parent := self.parent
	for ; parent.qualifiesForPathCompression(); parent = parent.parent {
	}
	parentBit, err := parent.targetBitFromIp(self.network.number)
	if err != nil {
		return err
	}
	parent.children[parentBit] = loneChild

	// Attempts to furthur apply path compression at current lineage parent, in case current lineage
	// compressed into parent.
	return parent.compressPathIfPossible()
}

func (self *trie) childrenCount() int {
	count := 0
	for _, child := range self.children {
		if child != nil {
			count++
		}
	}
	return count
}

func (self *trie) totalNumberOfBits() uint {
	return bitsPerUint32 * uint(len(self.network.number))
}

func (self *trie) targetBitPosition() int {
	return int(self.totalNumberOfBits()-self.bitsSkipped) - 1
}

func (self *trie) targetBitFromIp(n networkNumber) (uint32, error) {
	// This is a safe uint boxing of int since we should never attempt to get
	// target bit at a negative position.
	return n.Bit(uint(self.targetBitPosition()))
}

func (self *trie) hasEntry() bool {
	return self.entry != nil
}

func (self *trie) level() int {
	if self.parent == nil {
		return 0
	}
	return self.parent.level() + 1
}

func (self *trie) WalkDepth() <-chan Entry {
	entries := make(chan Entry)
	go func() {
		defer deferutil.Recover()
		if self.hasEntry() {
			entries <- self.entry
		}
		childEntriesList := []<-chan Entry{}
		for _, child := range self.children {
			if child == nil {
				continue
			}
			childEntriesList = append(childEntriesList, child.WalkDepth())
		}
		for _, childEntries := range childEntriesList {
			for entry := range childEntries {
				entries <- entry
			}
		}
		close(entries)
	}()
	return entries
}

type entry struct {
	ipNet net.IPNet
}

func (self *entry) Network() net.IPNet {
	return self.ipNet
}

// NewEntry returns a basic Entry that only stores the network itself.
func NewEntry(ipNet net.IPNet) Entry {
	return &entry{
		ipNet: ipNet,
	}
}
