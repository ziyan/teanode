package passwords

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrCardNotUsableByAgent is an attempt to open a card for the agent. A card
// is never given to the agent: paying stays the person's act.
var ErrCardNotUsableByAgent = errors.New("passwords: a card cannot be used by the agent")

// AgentOpener opens the items a person let the agent use. It holds the
// server's agent key, whose public half the person's device sealed each
// granted item's key for (ShareData with AgentRecipientID), and so it can open
// those items and no other: every other item's key is sealed for keys only the
// person's devices hold.
type AgentOpener struct {
	keyPair *KeyPair
}

// AgentItem is an item as the database stores it, with what opening it needs
// beyond the agent key: who owns it, and the owner's public key, because a key
// sealed for another key pair only opens with the public key of the owner who
// sealed it.
type AgentItem struct {
	ID                string
	Kind              ItemKind
	OwnerID           string
	OwnerPublicKey    []byte
	EncryptedFields   string
	ItemKeyByAgentKey string
}

// NewAgentOpener makes an opener from the 32 byte private scalar of the
// server's agent key.
func NewAgentOpener(agentPrivateKey []byte) (*AgentOpener, error) {
	keyPair, err := KeyPairFromPrivateKey(agentPrivateKey)
	if err != nil {
		return nil, err
	}
	return &AgentOpener{keyPair: keyPair}, nil
}

// OpenItemKey opens the item key a person's device sealed for the agent key.
// The item's id and owner are in the associated data, so a key sealed for one
// item or owner does not open another.
func (self *AgentOpener) OpenItemKey(item AgentItem) ([]byte, error) {
	sealed, err := ParseSealedForKeyPair(item.ItemKeyByAgentKey)
	if err != nil {
		return nil, err
	}
	return self.keyPair.Open(sealed, item.OwnerPublicKey, ShareData(item.ID, item.OwnerID, AgentRecipientID))
}

// OpenFields opens an item's fields and returns their JSON. It refuses a card
// before it opens anything.
func (self *AgentOpener) OpenFields(item AgentItem) ([]byte, error) {
	if item.Kind == ItemKindCard {
		return nil, ErrCardNotUsableByAgent
	}
	if !item.Kind.IsValid() {
		return nil, fmt.Errorf("passwords: %q is not a kind of item", item.Kind)
	}
	itemKey, err := self.OpenItemKey(item)
	if err != nil {
		return nil, err
	}
	sealed, err := ParseSealed(item.EncryptedFields)
	if err != nil {
		return nil, err
	}
	return Open(itemKey, sealed, ItemFieldsData(item.ID, item.Kind))
}

// OpenLogin opens a login's fields. It refuses an item of another kind, a card
// with the error of a card.
func (self *AgentOpener) OpenLogin(item AgentItem) (*LoginFields, error) {
	fields := &LoginFields{}
	if err := self.openInto(item, ItemKindLogin, fields); err != nil {
		return nil, err
	}
	return fields, nil
}

// OpenNote opens a note's fields. It refuses an item of another kind, a card
// with the error of a card.
func (self *AgentOpener) OpenNote(item AgentItem) (*NoteFields, error) {
	fields := &NoteFields{}
	if err := self.openInto(item, ItemKindNote, fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func (self *AgentOpener) openInto(item AgentItem, wantedKind ItemKind, fields any) error {
	if item.Kind != wantedKind && item.Kind != ItemKindCard {
		return fmt.Errorf("passwords: item %s is a %s, not a %s", item.ID, item.Kind, wantedKind)
	}
	plaintext, err := self.OpenFields(item)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(plaintext, fields); err != nil {
		return fmt.Errorf("passwords: read the fields of %s: %w", item.ID, err)
	}
	return nil
}
