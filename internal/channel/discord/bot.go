package discord

import (
	"context"
	"strings"

	"github.com/ziyan/teanode/internal/channel"
)

// Bot is a Discord bot as the channel manager runs it: what is said to
// it in a direct message, or with a mention in a server channel, handed
// over, and a Chat per channel to answer through.
type Bot struct {
	client   *Client
	identity *User
}

// Open is a channel.Opener for Discord: the token is tried at once.
func Open(ctx context.Context, token string) (channel.Bot, error) {
	return OpenAt(ctx, token, "", "")
}

// OpenAt is Open against other addresses, for tests.
func OpenAt(ctx context.Context, token, base, gateway string) (channel.Bot, error) {
	client := New(token, nil, base, gateway)
	identity, err := client.Me(ctx)
	if err != nil {
		return nil, err
	}
	return &Bot{client: client, identity: identity}, nil
}

func (self *Bot) Name() string {
	if self.identity.Username != "" {
		return "@" + self.identity.Username
	}
	return self.identity.Name()
}

// Run listens on the gateway until the context ends. In a server channel
// the bot answers what mentions it; in a direct message, everything. A
// direct message's chat is the person; a server channel's is the
// channel — the link names whichever sent the code.
func (self *Bot) Run(ctx context.Context, handle func(ctx context.Context, incoming *channel.Incoming, chat channel.Chat)) error {
	return self.client.Listen(ctx, func(message *Message) {
		if message.Author == nil || message.Author.Bot || message.Author.ID == self.identity.ID {
			return
		}
		incoming := self.incoming(message)
		if incoming.Group && !incoming.ToBot {
			if !strings.HasPrefix(strings.TrimSpace(incoming.Text), "/") {
				return
			}
		}
		if incoming.Text == "" && len(incoming.Files) == 0 {
			return
		}
		go handle(ctx, incoming, &chat{client: self.client, channelId: message.ChannelID})
	})
}

func (self *Bot) incoming(message *Message) *channel.Incoming {
	text := message.Content
	mentioned := false
	for _, user := range message.Mentions {
		if user.ID == self.identity.ID {
			mentioned = true
		}
	}
	text = strings.ReplaceAll(text, "<@"+self.identity.ID+">", "")
	text = strings.ReplaceAll(text, "<@!"+self.identity.ID+">", "")
	if message.ReferencedMessage != nil && message.ReferencedMessage.Author != nil && message.ReferencedMessage.Author.ID == self.identity.ID {
		mentioned = true
	}
	incoming := &channel.Incoming{
		ChatID:     message.ChannelID,
		ChatName:   "#" + message.ChannelID,
		SenderID:   message.Author.ID,
		SenderName: message.Author.Name(),
		MessageID:  message.ID,
		Text:       strings.TrimSpace(text),
		Group:      message.GuildID != "",
		ToBot:      mentioned,
	}
	if !incoming.Group {
		incoming.ChatName = message.Author.Name()
	}
	for _, attachment := range message.Attachments {
		address := attachment.URL
		incoming.Files = append(incoming.Files, channel.IncomingFile{
			Name: attachment.Filename, ContentType: attachment.ContentType,
			Fetch: func(ctx context.Context) ([]byte, error) { return self.client.Download(ctx, address) },
		})
	}
	return incoming
}

// chat is one Discord channel as the turn writes into it.
type chat struct {
	client    *Client
	channelId string
}

func (self *chat) Send(ctx context.Context, text, replyTo string) (string, error) {
	return self.client.Send(ctx, self.channelId, text, replyTo)
}

func (self *chat) Edit(ctx context.Context, messageId, text string) error {
	return self.client.Edit(ctx, self.channelId, messageId, text)
}

func (self *chat) Delete(ctx context.Context, messageId string) error {
	return self.client.Delete(ctx, self.channelId, messageId)
}

func (self *chat) Typing(ctx context.Context) error {
	return self.client.Typing(ctx, self.channelId)
}

func (self *chat) SendFile(ctx context.Context, name, contentType string, content []byte, caption string) error {
	return self.client.SendFile(ctx, self.channelId, name, contentType, content, caption)
}

func (self *chat) Limit() int { return MessageLimit }
