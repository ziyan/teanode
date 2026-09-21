package trie_test

import (
	"net"
	"reflect"
	"testing"

	"github.com/ziyan/teanode/internal/util/dropper/trie"
)

type ipVersion string

const (
	ipv4 ipVersion = "ipv4"
	ipv6 ipVersion = "ipv6"
)

func newTrie(version ipVersion) trie.Trie {
	switch version {
	case ipv4:
		return trie.New4()
	case ipv6:
		return trie.New6()
	}
	panic("unknown ip version")
}

func mustParseCidr(value string) *net.IPNet {
	_, cidr, err := net.ParseCIDR(value)
	if err != nil {
		panic(err)
	}
	return cidr
}

// allIpv4 is a IPv4 CIDR that contains all networks
var allIpv4 = mustParseCidr("0.0.0.0/0")

// allIpv6 is a IPv6 CIDR that contains all networks
var allIpv6 = mustParseCidr("0::0/0")

func getAllByVersion(version ipVersion) *net.IPNet {
	if version == ipv6 {
		return allIpv6
	}
	return allIpv4
}

func TestInsert(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version                      ipVersion
		inserts                      []string
		expectedNetworksInDepthOrder []string
		name                         string
	}{
		{ipv4, []string{"192.168.0.1/24"}, []string{"192.168.0.1/24"}, "basic insert"},
		{
			ipv4,
			[]string{"1.2.3.4/32", "1.2.3.5/32"},
			[]string{"1.2.3.4/32", "1.2.3.5/32"},
			"single ip IPv4 network insert",
		},
		{
			ipv6,
			[]string{"0::1/128", "0::2/128"},
			[]string{"0::1/128", "0::2/128"},
			"single ip IPv6 network insert",
		},
		{
			ipv4,
			[]string{"192.168.0.1/16", "192.168.0.1/24"},
			[]string{"192.168.0.1/16", "192.168.0.1/24"},
			"in order insert",
		},
		{
			ipv4,
			[]string{"192.168.0.1/32", "192.168.0.1/32"},
			[]string{"192.168.0.1/32"},
			"duplicate network insert",
		},
		{
			ipv4,
			[]string{"192.168.0.1/24", "192.168.0.1/16"},
			[]string{"192.168.0.1/16", "192.168.0.1/24"},
			"reverse insert",
		},
		{
			ipv4,
			[]string{"192.168.0.1/24", "192.168.1.1/24"},
			[]string{"192.168.0.1/24", "192.168.1.1/24"},
			"branch insert",
		},
		{
			ipv4,
			[]string{"192.168.0.1/24", "192.168.1.1/24", "192.168.1.1/30"},
			[]string{"192.168.0.1/24", "192.168.1.1/24", "192.168.1.1/30"},
			"branch inserts",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			self := newTrie(tc.version)
			for _, insert := range tc.inserts {
				_, network, _ := net.ParseCIDR(insert)
				err := self.Insert(trie.NewEntry(*network))
				if err != nil {
					t.Fatalf("error: %s", err)
				}
			}

			if len(tc.expectedNetworksInDepthOrder) != self.Len() {
				t.Fatalf("trie size should match")
			}

			allNetworks, err := self.CoveredNetworks(*getAllByVersion(tc.version))
			if err != nil {
				t.Fatalf("error: %s", err)
			}
			if len(allNetworks) != self.Len() {
				t.Fatalf("trie size should match")
			}

			walk := self.WalkDepth()
			for _, network := range tc.expectedNetworksInDepthOrder {
				_, ipnet, _ := net.ParseCIDR(network)
				expected := trie.NewEntry(*ipnet)
				actual := <-walk
				if !reflect.DeepEqual(expected, actual) {
					t.Fatalf("%v != %v", expected, actual)
				}
			}

			// Ensure no unexpected elements in trie.
			for network := range walk {
				if network != nil {
					t.Fatalf("network: %v", network)
				}
			}
		})
	}
}

func TestString(t *testing.T) {
	t.Parallel()
	inserts := []string{"192.168.0.1/24", "192.168.1.1/24", "192.168.1.1/30"}
	self := newTrie(ipv4)
	for _, insert := range inserts {
		_, network, _ := net.ParseCIDR(insert)
		if err := self.Insert(trie.NewEntry(*network)); err != nil {
			t.Fatalf("failed to insert: %s", err)
		}
	}
	expected := `0.0.0.0/0 (target_pos:31:has_entry:false)
| 1--> 192.168.0.0/23 (target_pos:8:has_entry:false)
| | 0--> 192.168.0.0/24 (target_pos:7:has_entry:true)
| | 1--> 192.168.1.0/24 (target_pos:7:has_entry:true)
| | | 0--> 192.168.1.0/30 (target_pos:1:has_entry:true)`
	if expected != self.String() {
		t.Fatalf("not equal")
	}
}

func TestRemove(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version                      ipVersion
		inserts                      []string
		removes                      []string
		expectedRemoves              []string
		expectedNetworksInDepthOrder []string
		expectedTrieString           string
		name                         string
	}{
		{
			ipv4,
			[]string{"192.168.0.1/24"},
			[]string{"192.168.0.1/24"},
			[]string{"192.168.0.1/24"},
			[]string{},
			"0.0.0.0/0 (target_pos:31:has_entry:false)",
			"basic remove",
		},
		{
			ipv4,
			[]string{"192.168.0.1/32"},
			[]string{"192.168.0.1/24"},
			[]string{""},
			[]string{"192.168.0.1/32"},
			`0.0.0.0/0 (target_pos:31:has_entry:false)
| 1--> 192.168.0.1/32 (target_pos:-1:has_entry:true)`,
			"remove from ranger that contains a single ip block",
		},
		{
			ipv4,
			[]string{"1.2.3.4/32", "1.2.3.5/32"},
			[]string{"1.2.3.5/32"},
			[]string{"1.2.3.5/32"},
			[]string{"1.2.3.4/32"},
			`0.0.0.0/0 (target_pos:31:has_entry:false)
| 0--> 1.2.3.4/32 (target_pos:-1:has_entry:true)`,
			"single ip IPv4 network remove",
		},
		{
			ipv4,
			[]string{"0::1/128", "0::2/128"},
			[]string{"0::2/128"},
			[]string{"0::2/128"},
			[]string{"0::1/128"},
			`0.0.0.0/0 (target_pos:31:has_entry:false)
| 0--> ::1/128 (target_pos:-1:has_entry:true)`,
			"single ip IPv6 network remove",
		},
		{
			ipv4,
			[]string{"192.168.0.1/24", "192.168.0.1/25", "192.168.0.1/26"},
			[]string{"192.168.0.1/25"},
			[]string{"192.168.0.1/25"},
			[]string{"192.168.0.1/24", "192.168.0.1/26"},
			`0.0.0.0/0 (target_pos:31:has_entry:false)
| 1--> 192.168.0.0/24 (target_pos:7:has_entry:true)
| | 0--> 192.168.0.0/26 (target_pos:5:has_entry:true)`,
			"remove path prefix",
		},
		{
			ipv4,
			[]string{"192.168.0.1/24", "192.168.0.1/25", "192.168.0.64/26", "192.168.0.1/26"},
			[]string{"192.168.0.1/25"},
			[]string{"192.168.0.1/25"},
			[]string{"192.168.0.1/24", "192.168.0.1/26", "192.168.0.64/26"},
			`0.0.0.0/0 (target_pos:31:has_entry:false)
| 1--> 192.168.0.0/24 (target_pos:7:has_entry:true)
| | 0--> 192.168.0.0/25 (target_pos:6:has_entry:false)
| | | 0--> 192.168.0.0/26 (target_pos:5:has_entry:true)
| | | 1--> 192.168.0.64/26 (target_pos:5:has_entry:true)`,
			"remove path prefix with more than 1 children",
		},
		{
			ipv4,
			[]string{"192.168.0.1/24", "192.168.0.1/25"},
			[]string{"192.168.0.1/26"},
			[]string{""},
			[]string{"192.168.0.1/24", "192.168.0.1/25"},
			`0.0.0.0/0 (target_pos:31:has_entry:false)
| 1--> 192.168.0.0/24 (target_pos:7:has_entry:true)
| | 0--> 192.168.0.0/25 (target_pos:6:has_entry:true)`,
			"remove non existent",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			self := newTrie(tc.version)
			for _, insert := range tc.inserts {
				_, network, _ := net.ParseCIDR(insert)
				err := self.Insert(trie.NewEntry(*network))
				if err != nil {
					t.Fatalf("error: %s", err)
				}
			}
			for i, remove := range tc.removes {
				_, network, _ := net.ParseCIDR(remove)
				removed, err := self.Remove(*network)
				if err != nil {
					t.Fatalf("error: %s", err)
				}
				if str := tc.expectedRemoves[i]; str != "" {
					_, ipnet, _ := net.ParseCIDR(str)
					expected := trie.NewEntry(*ipnet)
					if !reflect.DeepEqual(expected, removed) {
						t.Fatalf("not equal")
					}
				} else if removed != nil {
					t.Fatalf("removed: %v", removed)
				}
			}

			if len(tc.expectedNetworksInDepthOrder) != self.Len() {
				t.Fatalf("trie size should match after revmoval")
			}

			allNetworks, err := self.CoveredNetworks(*getAllByVersion(tc.version))
			if err != nil {
				t.Fatalf("error: %s", err)
			}
			if len(allNetworks) != self.Len() {
				t.Fatalf("trie size should match")
			}

			walk := self.WalkDepth()
			for _, network := range tc.expectedNetworksInDepthOrder {
				_, ipnet, _ := net.ParseCIDR(network)
				expected := trie.NewEntry(*ipnet)
				actual := <-walk
				if !reflect.DeepEqual(expected, actual) {
					t.Fatalf("not equal")
				}
			}

			// Ensure no unexpected elements in trie.
			for network := range walk {
				if network != nil {
					t.Fatalf("network: %v", network)
				}
			}

			if tc.expectedTrieString != self.String() {
				t.Fatalf("not equal")
			}
		})
	}
}

func TestToReplicateIssue(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version  ipVersion
		inserts  []string
		ip       net.IP
		networks []string
		name     string
	}{
		{
			ipv4,
			[]string{"192.168.0.1/32"},
			net.ParseIP("192.168.0.1"),
			[]string{"192.168.0.1/32"},
			"basic containing network for /32 mask",
		},
		{
			ipv6,
			[]string{"a::1/128"},
			net.ParseIP("a::1"),
			[]string{"a::1/128"},
			"basic containing network for /128 mask",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			self := newTrie(tc.version)
			for _, insert := range tc.inserts {
				_, network, _ := net.ParseCIDR(insert)
				err := self.Insert(trie.NewEntry(*network))
				if err != nil {
					t.Fatalf("error: %s", err)
				}
			}
			expectedEntries := []trie.Entry{}
			for _, network := range tc.networks {
				_, net, _ := net.ParseCIDR(network)
				expectedEntries = append(expectedEntries, trie.NewEntry(*net))
			}
			contains, err := self.Contains(tc.ip)
			if err != nil {
				t.Fatalf("error: %s", err)
			}
			if !contains {
				t.Fatalf("not true")
			}
			networks, err := self.ContainingNetworks(tc.ip)
			if err != nil {
				t.Fatalf("error: %s", err)
			}
			if !reflect.DeepEqual(expectedEntries, networks) {
				t.Fatalf("not equal")
			}
		})
	}
}

type expectedIPRange struct {
	start net.IP
	end   net.IP
}

func TestContains(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version     ipVersion
		inserts     []string
		expectedIPs []expectedIPRange
		name        string
	}{
		{
			ipv4,
			[]string{"192.168.0.0/24"},
			[]expectedIPRange{
				{net.ParseIP("192.168.0.0"), net.ParseIP("192.168.1.0")},
			},
			"basic contains",
		},
		{
			ipv4,
			[]string{"192.168.0.0/24", "128.168.0.0/24"},
			[]expectedIPRange{
				{net.ParseIP("192.168.0.0"), net.ParseIP("192.168.1.0")},
				{net.ParseIP("128.168.0.0"), net.ParseIP("128.168.1.0")},
			},
			"multiple ranges contains",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			self := newTrie(tc.version)
			for _, insert := range tc.inserts {
				_, network, _ := net.ParseCIDR(insert)
				err := self.Insert(trie.NewEntry(*network))
				if err != nil {
					t.Fatalf("error: %s", err)
				}
			}
			for _, expectedIPRange := range tc.expectedIPs {
				var contains bool
				var err error
				start := expectedIPRange.start
				for ; !expectedIPRange.end.Equal(start); start = trie.NextIP(start) {
					contains, err = self.Contains(start)
					if err != nil {
						t.Fatalf("error: %s", err)
					}
					if !contains {
						t.Fatalf("not true")
					}
				}

				// Check out of bounds ips on both ends
				contains, err = self.Contains(trie.PreviousIP(expectedIPRange.start))
				if err != nil {
					t.Fatalf("error: %s", err)
				}
				if contains {
					t.Fatalf("not false")
				}
				contains, err = self.Contains(trie.NextIP(expectedIPRange.end))
				if err != nil {
					t.Fatalf("error: %s", err)
				}
				if contains {
					t.Fatalf("not false")
				}
			}
		})
	}
}

func TestContainingNetworks(t *testing.T) {
	t.Parallel()
	cases := []struct {
		version  ipVersion
		inserts  []string
		ip       net.IP
		networks []string
		name     string
	}{
		{
			ipv4,
			[]string{"192.168.0.0/24"},
			net.ParseIP("192.168.0.1"),
			[]string{"192.168.0.0/24"},
			"basic containing networks",
		},
		{
			ipv4,
			[]string{"192.168.0.0/24", "192.168.0.0/25"},
			net.ParseIP("192.168.0.1"),
			[]string{"192.168.0.0/24", "192.168.0.0/25"},
			"inclusive networks",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			self := newTrie(tc.version)
			for _, insert := range tc.inserts {
				_, network, _ := net.ParseCIDR(insert)
				err := self.Insert(trie.NewEntry(*network))
				if err != nil {
					t.Fatalf("error: %s", err)
				}
			}
			expectedEntries := []trie.Entry{}
			for _, network := range tc.networks {
				_, net, _ := net.ParseCIDR(network)
				expectedEntries = append(expectedEntries, trie.NewEntry(*net))
			}
			networks, err := self.ContainingNetworks(tc.ip)
			if err != nil {
				t.Fatalf("error: %s", err)
			}
			if !reflect.DeepEqual(expectedEntries, networks) {
				t.Fatalf("not equal")
			}
		})
	}
}

type coveredNetworkTest struct {
	version  ipVersion
	inserts  []string
	search   string
	networks []string
	name     string
}

var coveredNetworkTests = []coveredNetworkTest{
	{
		ipv4,
		[]string{"192.168.0.0/24"},
		"192.168.0.0/16",
		[]string{"192.168.0.0/24"},
		"basic covered networks",
	},
	{
		ipv4,
		[]string{"192.168.0.0/24"},
		"10.1.0.0/16",
		nil,
		"nothing",
	},
	{
		ipv4,
		[]string{"192.168.0.0/24", "192.168.0.0/25"},
		"192.168.0.0/16",
		[]string{"192.168.0.0/24", "192.168.0.0/25"},
		"multiple networks",
	},
	{
		ipv4,
		[]string{"192.168.0.0/24", "192.168.0.0/25", "192.168.0.1/32"},
		"192.168.0.0/16",
		[]string{"192.168.0.0/24", "192.168.0.0/25", "192.168.0.1/32"},
		"multiple networks 2",
	},
	{
		ipv4,
		[]string{"192.168.1.1/32"},
		"192.168.0.0/16",
		[]string{"192.168.1.1/32"},
		"leaf",
	},
	{
		ipv4,
		[]string{"0.0.0.0/0", "192.168.1.1/32"},
		"192.168.0.0/16",
		[]string{"192.168.1.1/32"},
		"leaf with root",
	},
	{
		ipv4,
		[]string{
			"0.0.0.0/0", "192.168.0.0/24", "192.168.1.1/32",
			"10.1.0.0/16", "10.1.1.0/24",
		},
		"192.168.0.0/16",
		[]string{"192.168.0.0/24", "192.168.1.1/32"},
		"path not taken",
	},
	{
		ipv4,
		[]string{
			"192.168.0.0/15",
		},
		"192.168.0.0/16",
		nil,
		"only masks different",
	},
}

func TestCoveredNetworks(t *testing.T) {
	t.Parallel()
	for _, tc := range coveredNetworkTests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			self := newTrie(tc.version)
			for _, insert := range tc.inserts {
				_, network, _ := net.ParseCIDR(insert)
				err := self.Insert(trie.NewEntry(*network))
				if err != nil {
					t.Fatalf("error: %s", err)
				}
			}
			var expectedEntries []trie.Entry
			for _, network := range tc.networks {
				_, net, _ := net.ParseCIDR(network)
				expectedEntries = append(expectedEntries, trie.NewEntry(*net))
			}
			_, snet, _ := net.ParseCIDR(tc.search)
			networks, err := self.CoveredNetworks(*snet)
			if err != nil {
				t.Fatalf("error: %s", err)
			}
			if !reflect.DeepEqual(expectedEntries, networks) {
				t.Fatalf("not equal")
			}
		})
	}
}
