package apigraph

import (
	"context"
	"time"

	"github.com/ziyan/teanode/internal/api"
	"github.com/ziyan/teanode/internal/models"
)

type DeliveryQuery interface {
	// List Deliveries belonging to a Domain
	ListDeliveries(ctx context.Context, arguments ListDeliveriesArguments) ([]*models.Delivery, error)

	// Get a particular Delivery
	GetDelivery(ctx context.Context, arguments GetDeliveryArguments) (*models.Delivery, error)

	// List every Delivery made for one Mail, which is what happened to it
	ListDeliveriesByMail(ctx context.Context, arguments ListDeliveriesByMailArguments) ([]*models.Delivery, error)

	// List Deliveries that have not finished: still queued, being retried, or
	// waiting after a failure
	ListPendingDeliveries(ctx context.Context, arguments ListPendingDeliveriesArguments) ([]*models.Delivery, error)
}

type DeliveryMutation interface {
	// Retry a failed or attempted Delivery
	RetryDelivery(ctx context.Context, arguments RetryDeliveryArguments) (*models.Delivery, error)
}

type ListDeliveriesArguments struct {
	// ID of the Domain
	DomainID string `json:"domainId"`

	*api.Pagination `json:"pagination"`
}

func (self *graph) ListDeliveries(ctx context.Context, arguments ListDeliveriesArguments) ([]*models.Delivery, error) {
	domain, err := self.requireDomain(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	// list all deliveries for this domain
	deliveries, err := api.ContextTransaction(ctx).ListDeliveriesByDomainID(domain.ID, arguments.Options())
	if err != nil {
		return nil, err
	}

	return deliveries, nil
}

type GetDeliveryArguments struct {
	// ID of the Delivery to look up
	DeliveryID string `json:"deliveryId"`
}

func (self *graph) GetDelivery(ctx context.Context, arguments GetDeliveryArguments) (*models.Delivery, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	delivery, err := api.ContextTransaction(ctx).GetDelivery(arguments.DeliveryID, nil)
	if err != nil {
		return nil, err
	}
	if delivery == nil {
		return nil, api.ErrNotFound
	}

	// look up mail to find domain
	mail, err := api.ContextTransaction(ctx).GetMail(delivery.MailID, nil)
	if err != nil {
		return nil, err
	}
	if mail == nil {
		return nil, api.ErrNotFound
	}

	// the domain has to still be configured
	if self.config.Current().FindDomainByID(mail.DomainID) == nil {
		return nil, api.ErrNotFound
	}

	return delivery, nil
}

type RetryDeliveryArguments struct {
	// ID of the Delivery to retry
	DeliveryID string `json:"deliveryId"`
}

func (self *graph) RetryDelivery(ctx context.Context, arguments RetryDeliveryArguments) (*models.Delivery, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	delivery, err := api.ContextTransaction(ctx).GetDelivery(arguments.DeliveryID, nil)
	if err != nil {
		return nil, err
	}
	if delivery == nil {
		return nil, api.ErrNotFound
	}

	// look up mail to find domain
	mail, err := api.ContextTransaction(ctx).GetMail(delivery.MailID, nil)
	if err != nil {
		return nil, err
	}
	if mail == nil {
		return nil, api.ErrNotFound
	}

	// the domain has to still be configured
	if self.config.Current().FindDomainByID(mail.DomainID) == nil {
		return nil, api.ErrNotFound
	}

	// only allow retrying deliveries that are in a retryable state
	switch delivery.Status {
	case models.DeliveryStatusAttempted, models.DeliveryStatusFailed, models.DeliveryStatusDelayed:
		// these statuses can be retried
	default:
		return nil, api.ErrNotRetryable
	}

	// set retry_at to now so the periodic retry loop picks it up
	delivery, err = api.ContextTransaction(ctx).ModifyDelivery(arguments.DeliveryID, func(d *models.Delivery) error {
		now := time.Now().In(time.Local)
		d.RetryAt = &now
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}

	return delivery, nil
}

type ListDeliveriesByMailArguments struct {
	// ID of the Mail
	MailID string `json:"mailId"`
}

func (self *graph) ListDeliveriesByMail(ctx context.Context, arguments ListDeliveriesByMailArguments) ([]*models.Delivery, error) {
	if err := self.requireOperator(ctx); err != nil {
		return nil, err
	}

	mail, err := api.ContextTransaction(ctx).GetMail(arguments.MailID, nil)
	if err != nil {
		return nil, err
	}
	if mail == nil {
		return nil, api.ErrNotFound
	}
	return api.ContextTransaction(ctx).ListDeliveries([]string{mail.ID}, nil)
}

type ListPendingDeliveriesArguments struct {
	// ID of the Domain, or empty for every configured domain
	DomainID string `json:"domainId"`

	*api.Pagination `json:"pagination"`
}

// ListPendingDeliveries is the queue view: what has not been delivered yet and
// why. An operator looking for "is my mail stuck" is looking for this.
func (self *graph) ListPendingDeliveries(ctx context.Context, arguments ListPendingDeliveriesArguments) ([]*models.Delivery, error) {
	domainIds, err := self.domainsToList(ctx, arguments.DomainID)
	if err != nil {
		return nil, err
	}

	var pending []*models.Delivery
	for _, domainId := range domainIds {
		deliveries, err := api.ContextTransaction(ctx).ListDeliveriesByDomainID(domainId, arguments.Options())
		if err != nil {
			return nil, err
		}
		for _, delivery := range deliveries {
			switch delivery.Status {
			case models.DeliveryStatusDelivered, models.DeliveryStatusDropped:
				continue
			}
			pending = append(pending, delivery)
		}
	}
	return pending, nil
}
