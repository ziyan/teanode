package models

import "time"

// AgentFollowUpKind is what sort of situation a follow-up keeps track of.
type AgentFollowUpKind string

// The kinds: a bill with a payee, an amount and a due date; a parcel with a
// carrier and a tracking number; something the person must do or return by
// a date, such as a form or an RSVP; and a dated happening they may want to
// go to. They differ in what closes them and when to speak.
const (
	AgentFollowUpBill     AgentFollowUpKind = "bill"
	AgentFollowUpParcel   AgentFollowUpKind = "parcel"
	AgentFollowUpDeadline AgentFollowUpKind = "deadline"
	AgentFollowUpEvent    AgentFollowUpKind = "event"
)

// IsValid says the kind is one of the four.
func (self AgentFollowUpKind) IsValid() bool {
	switch self {
	case AgentFollowUpBill, AgentFollowUpParcel, AgentFollowUpDeadline, AgentFollowUpEvent:
		return true
	}
	return false
}

// AgentFollowUpState is where a follow-up stands.
type AgentFollowUpState string

// The states: open, until something closes it; closed, by later mail or a
// payment that shows it happened; overdue, a bill past its date with no
// payment seen; stalled, a parcel well past its expected date; expired, a
// deadline or event whose day has gone; and stopped, which the person said
// not to follow any more.
const (
	AgentFollowUpOpen    AgentFollowUpState = "open"
	AgentFollowUpClosed  AgentFollowUpState = "closed"
	AgentFollowUpOverdue AgentFollowUpState = "overdue"
	AgentFollowUpStalled AgentFollowUpState = "stalled"
	AgentFollowUpExpired AgentFollowUpState = "expired"
	AgentFollowUpStopped AgentFollowUpState = "stopped"
)

// IsValid says the state is one of the six.
func (self AgentFollowUpState) IsValid() bool {
	switch self {
	case AgentFollowUpOpen, AgentFollowUpClosed, AgentFollowUpOverdue, AgentFollowUpStalled, AgentFollowUpExpired, AgentFollowUpStopped:
		return true
	}
	return false
}

// IsOpen says the follow-up is still being watched: open, overdue or
// stalled. The others are over.
func (self AgentFollowUpState) IsOpen() bool {
	switch self {
	case AgentFollowUpOpen, AgentFollowUpOverdue, AgentFollowUpStalled:
		return true
	}
	return false
}

// AgentFollowUpToldState is what the person was last told about a follow-up,
// kept apart from the follow-up's own state: a bill is reminded while it is
// still open, and a delivered parcel is told about as it closes.
type AgentFollowUpToldState string

// The things told: a bill or deadline reminded before its day, a bill
// overdue, a parcel stalled, a parcel delivered, and an event or deadline
// that went out in the Sunday roundup. A follow-up is told about each of
// them once.
const (
	AgentFollowUpToldReminded  AgentFollowUpToldState = "reminded"
	AgentFollowUpToldOverdue   AgentFollowUpToldState = "overdue"
	AgentFollowUpToldStalled   AgentFollowUpToldState = "stalled"
	AgentFollowUpToldDelivered AgentFollowUpToldState = "delivered"
	AgentFollowUpToldRoundup   AgentFollowUpToldState = "roundup"
)

// IsValid says the told state is one of the five.
func (self AgentFollowUpToldState) IsValid() bool {
	switch self {
	case AgentFollowUpToldReminded, AgentFollowUpToldOverdue, AgentFollowUpToldStalled, AgentFollowUpToldDelivered, AgentFollowUpToldRoundup:
		return true
	}
	return false
}

// AgentFollowUp is a thing mail said is due, on its way or coming up, kept
// until something closes it.
type AgentFollowUp struct {
	ID      string `json:"id"`
	AgentID string `json:"agentId"`

	FollowUpKind AgentFollowUpKind `json:"followUpKind"`

	// FollowUpKey is what the same situation always produces, so a later
	// message about it finds this follow-up: the tracking number of a
	// parcel, the payee and due date of a bill.
	FollowUpKey   string `json:"followUpKey"`
	FollowUpTitle string `json:"followUpTitle"`

	Payee   string `json:"payee,omitempty" graphapi:"nullable"`
	Carrier string `json:"carrier,omitempty" graphapi:"nullable"`

	// Amount is a decimal number as a string, such as "1234.00", in
	// CurrencyCode, as FinanceTransaction.Amount is.
	Amount         string `json:"amount,omitempty" graphapi:"nullable"`
	CurrencyCode   string `json:"currencyCode,omitempty" graphapi:"nullable"`
	TrackingNumber string `json:"trackingNumber,omitempty" graphapi:"nullable"`

	// DueOn is a day written 2006-01-02, empty when the message gave none.
	DueOn string `json:"dueOn,omitempty" graphapi:"nullable"`
	Place string `json:"place,omitempty" graphapi:"nullable"`

	FollowUpState AgentFollowUpState `json:"followUpState"`
	ClosedReason  string             `json:"closedReason,omitempty" graphapi:"nullable"`

	// SourceItemIDs are the messages it came from, newest last.
	SourceItemIDs []string `json:"sourceItemIds"`

	// ToldState is what the person was last told about it, empty when
	// never, and ToldAt when.
	ToldState AgentFollowUpToldState `json:"toldState,omitempty" graphapi:"nullable"`
	ToldAt    *time.Time             `json:"toldAt,omitempty" graphapi:"nullable"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
