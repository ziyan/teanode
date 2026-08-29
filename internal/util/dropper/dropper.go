// Package dropper provides IP-based connection dropping using trie lookups.
package dropper

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"

	"github.com/ziyan/teanode/internal/util/dropper/trie"
	"github.com/ziyan/teanode/internal/util/periodic"
)

var log = logging.MustGetLogger("dropper")

type Dropper interface {
	Close() error

	Drop(ip net.IP) (bool, error)
}

type dropper struct {
	waitGroup sync.WaitGroup
	periodic  periodic.Periodic

	mutex sync.Mutex
	tree4 trie.Trie
	tree6 trie.Trie
}

func Open() (Dropper, error) {
	self := &dropper{
		tree4: trie.New4(),
		tree6: trie.New6(),
	}
	self.periodic = periodic.New(context.TODO(), &self.waitGroup, self.spinOnce, &periodic.Settings{
		Interval: 2 * time.Hour,
		Name:     "dropper",
	})
	self.periodic.Start()
	return self, nil
}

func (self *dropper) Close() error {
	self.periodic.Stop()
	self.waitGroup.Wait()
	return nil
}

func (self *dropper) Drop(ip net.IP) (bool, error) {
	tree4, tree6 := self.getTrees()
	if ip.To4() != nil {
		return tree4.Contains(ip)
	} else {
		return tree6.Contains(ip)
	}
}

func (self *dropper) getTrees() (trie.Trie, trie.Trie) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	return self.tree4, self.tree6
}

func (self *dropper) setTrees(tree4, tree6 trie.Trie) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	self.tree4 = tree4
	self.tree6 = tree6
}

func (self *dropper) spinOnce(ctx context.Context) error {
	tree4 := trie.New4()
	for _, url := range []string{
		"https://www.spamhaus.org/drop/drop.txt",
		"https://www.spamhaus.org/drop/edrop.txt",
	} {
		if err := self.addList(ctx, tree4, url); err != nil {
			return err
		}
	}
	tree6 := trie.New6()
	for _, url := range []string{
		"https://www.spamhaus.org/drop/dropv6.txt",
	} {
		if err := self.addList(ctx, tree6, url); err != nil {
			return err
		}
	}
	log.Warningf("%d entries in ipv4 tree and %d entries in ipv6 tree", tree4.Len(), tree6.Len())
	self.setTrees(tree4, tree6)
	return nil
}

func (self *dropper) addList(ctx context.Context, tree trie.Trie, url string) error {
	request, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()

	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}
		parts := strings.Split(line, ";")
		cidr := strings.TrimSpace(parts[0])
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Warningf("failed to parse cidr %q: %s", cidr, err)
			continue
		}
		if err := tree.Insert(trie.NewEntry(*network)); err != nil {
			log.Warningf("failed to insert cidr %q: %s", cidr, err)
			continue
		}
	}
	return scanner.Err()
}
