package client

import (
	"context"
	"time"
)

// Delivery is one attempt to pass a message on: to an address, a webhook or
// another server.
type Delivery struct {
	ID          string     `json:"id"`
	CreatedAt   time.Time  `json:"createdAt"`
	MailID      string     `json:"mailId"`
	DomainID    string     `json:"domainId"`
	AliasID     string     `json:"aliasId"`
	Recipient   string     `json:"recipient"`
	Kind        string     `json:"kind"`
	Status      string     `json:"status"`
	Size        uint64     `json:"size"`
	AttemptedAt *time.Time `json:"attemptedAt"`
	DeliveredAt *time.Time `json:"deliveredAt"`
	DroppedAt   *time.Time `json:"droppedAt"`
	RetryAt     *time.Time `json:"retryAt"`
	Attempts    uint64     `json:"attempts"`
	Error       string     `json:"error"`
}

const deliveryFields = `{
	id createdAt mailId domainId aliasId recipient kind status size
	attemptedAt deliveredAt droppedAt retryAt attempts error
}`

// ListDeliveries returns a domain's deliveries, newest first.
func ListDeliveries(ctx context.Context, connection *Client, domainId string, first int) ([]*Delivery, error) {
	var result struct {
		ListDeliveries []*Delivery `json:"ListDeliveries"`
	}
	query := `query ($domainId: String!, $pagination: PaginationInput) {
		ListDeliveries(domainId: $domainId, pagination: $pagination) ` + deliveryFields + `
	}`
	variables := map[string]any{"domainId": domainId, "pagination": pagination(first)}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ListDeliveries, nil
}

// ListPendingDeliveries returns what has not been delivered yet, for every
// domain when domainId is empty.
func ListPendingDeliveries(ctx context.Context, connection *Client, domainId string, first int) ([]*Delivery, error) {
	var result struct {
		ListPendingDeliveries []*Delivery `json:"ListPendingDeliveries"`
	}
	query := `query ($domainId: String!, $pagination: PaginationInput) {
		ListPendingDeliveries(domainId: $domainId, pagination: $pagination) ` + deliveryFields + `
	}`
	variables := map[string]any{"domainId": domainId, "pagination": pagination(first)}
	if err := connection.Execute(ctx, query, variables, &result); err != nil {
		return nil, err
	}
	return result.ListPendingDeliveries, nil
}

// ListDeliveriesByMail returns every delivery made for one message.
func ListDeliveriesByMail(ctx context.Context, connection *Client, mailId string) ([]*Delivery, error) {
	var result struct {
		ListDeliveriesByMail []*Delivery `json:"ListDeliveriesByMail"`
	}
	query := `query ($mailId: String!) { ListDeliveriesByMail(mailId: $mailId) ` + deliveryFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"mailId": mailId}, &result); err != nil {
		return nil, err
	}
	return result.ListDeliveriesByMail, nil
}

// GetDelivery returns one delivery.
func GetDelivery(ctx context.Context, connection *Client, deliveryId string) (*Delivery, error) {
	var result struct {
		GetDelivery *Delivery `json:"GetDelivery"`
	}
	query := `query ($deliveryId: String!) { GetDelivery(deliveryId: $deliveryId) ` + deliveryFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"deliveryId": deliveryId}, &result); err != nil {
		return nil, err
	}
	return result.GetDelivery, nil
}

// RetryDelivery asks for a failed or delayed delivery to be tried again now.
func RetryDelivery(ctx context.Context, connection *Client, deliveryId string) (*Delivery, error) {
	var result struct {
		RetryDelivery *Delivery `json:"RetryDelivery"`
	}
	query := `mutation ($deliveryId: String!) { RetryDelivery(deliveryId: $deliveryId) ` + deliveryFields + ` }`
	if err := connection.Execute(ctx, query, map[string]any{"deliveryId": deliveryId}, &result); err != nil {
		return nil, err
	}
	return result.RetryDelivery, nil
}

// pagination is the PaginationInput for the first N rows, or nil for every
// row, which is what the server takes an absent argument to mean.
func pagination(first int) map[string]any {
	if first <= 0 {
		return nil
	}
	return map[string]any{"first": first}
}
