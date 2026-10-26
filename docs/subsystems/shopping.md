# Shopping lists

A person keeps named shopping lists here ("Groceries", "Hardware"), shares
them with a circle or a person, checks them off at the store with the others
watching live, and lets a receipt check off what it bought. Every item
remembers its price at each store over time, its picture is found on the web,
and the person's phone reminds them what their lists need when they arrive at
a store. This says how those fit together and which choices are load-bearing.
The plan with its decisions is `docs/planning/shopping-lists-execplan.md`.

## The pieces

`internal/db/database_shopping*.go` keeps the rows and decides who reaches a
list; migration 0260 makes the tables. `internal/api/v1api/apigraph/shopping.go`
is what every client uses: the dashboard (`web/src/pages/shopping/`), the
iPhone app, the command line (`teanode shopping`) and the agent's `shopping`
tool. `internal/agent/shopping.go` holds the two jobs (matching a receipt,
finding a picture) and the reading of a pasted or photographed list;
`internal/agent/shopping_notify.go` the two notifications.

## Lists, items and their states

A list belongs to one person. An item is on one list, with a description,
details (the preferences a shopper reads in the store), a count needed and a
unit, the store to get it from or none for any store, whether it is the
store's own brand, an aisle, a picture, and its latest price.

An item is in one of three states. **Needed** is to buy. **In the cart** is
checked off at the store: it stays there until the shopper's trip is finished,
a receipt matches it, or somebody starts a new trip on that list more than six
hours after the old one started (`models.ShoppingTripStaleAfter`); then it is
bought. **Not needed** is bought or not wanted now. A bought item stays on its
list with its picture, store and prices, so a list is reused week after week:
"Need again" makes it needed with a count. There is no count of what is at
home, by the maintainer's choice.

Adding an item already on the list, by its description and store compared
without case, needs the existing one again: its count becomes the larger of the
two, its details gain what is new, and no second row is made. So importing the
same weekly list twice does not double it.

Stores are compared by `models.ShoppingStoreKey`: lower case, letters and digits
only, so "Example Market", "EXAMPLE MARKET'S" and "example-market" are one
store, at any branch. Aisles keep the order they were given in, so a list
written in a store's order is shown in it.

## Who reaches a list

A list reaches its owner and whoever its shares reach: a share to a person while
the two share a circle, a share to a circle while both the reader and the owner
are in it, and only while the owner (and the reader) is enabled and has
`shopping:use`. A person without it reaches no list, their own included: they
are reminded of nothing, and no receipt of theirs checks anything off. One
query says it (`shoppingListReachQuery`), and every operation that takes a list
or an item asks it through `GetShoppingListAccess`. Leaving a circle deletes
the direct shares it made possible (`endShoppingSharesOutsideCircles`, called
with every other kind's from `endSharesOutsideCircles`).

There are no levels: everybody a list reaches may add, change and check off its
items, mark them out of stock, price them and upload receipts. Only the owner
shares it further or deletes it. Shares appear on the Sharing page with every
other kind, as `shopping_list`.

## Shopping a store

At the store a person shops a store, not a list. The store screen
(`ListShoppingStoreItems`) gathers the needed and in-cart items of every list
they reach that are for that store, and, unless asked otherwise, the items for
any store after them, by aisle; each item says which list it is on. Buttons
include or leave out each list. The same thing on two lists stays two rows,
because each list's own people must see only their list.

`StartShopping` opens a trip for the lists included and tells, on their
phones, everybody else those lists reach and who has not muted the list
("Alex started shopping Groceries", "At Example Market."), at most once in two
hours about the same shopper. Looking at a store does not start a trip; the
person presses Start shopping, or taps the notice at the store.
`FinishShopping` buys what the shopper put in the cart on every list they
reach, a list switched on at the store after the trip began included; an
item in another person's cart stays in theirs, and checking it again does not
take it over.

## Live

Every change announces the list on the PostgreSQL channel `shopping_event`
after its transaction commits. Each instance holds one listener while somebody
follows a list (`shoppingEventHub`), decides for each event who among the
screens open on it may read the list, and tells them; the client reads the list
again. A share that ends, or a deleted list, tells the people it stopped
reaching, so their screens drop it. A missed event is gone; the next read
catches up. This is the location hub's design.

## Receipts are one system

Every Finance receipt is stored through `Agent.RecordReceipt`, whatever it came
from: a photo uploaded on a list or in Finance, a mail sorted as a receipt, a
picture sent to the agent. That call queues the `match_shopping` job for the
receipt when the owner reaches any item a receipt from that merchant could
have bought (needed or in the cart, for a store the merchant's name contains,
or for any store). The job shows a model the receipt's item lines and those
items, numbered, both inside the untrusted fence, and asks which line bought
which item and whether it is sure.

A sure match checks its item off (bought, or still needed for what the line
did not buy of it) and records the line's price at the store, from the printed
unit price or the amount over the count or weight. An unsure one is only
proposed, for the person to apply or dismiss. Everything applying changes
about the item goes on the match first (its state, count, when it was last
bought, an out-of-stock mark), so "Undo the check-offs" puts exactly that back
and takes the price away; an item somebody changed after the match keeps the
newer change. A match is kept by the receipt and the line's number, because
reading a receipt again replaces its line rows: what was decided survives a
second reading. A line or an item already matched on a receipt is never
matched again by the job, and a dismissed pair is never proposed again, so
running the matcher twice changes nothing that was decided. The match is
queued after the receipt is stored, in a transaction of its own, so it never
costs the receipt. The merchant's name and lines are inside the untrusted
fence; the worst a hostile receipt can do is check off items that are needed
(each undoable), since the model has no tools and sees only the person's own
candidate items.

## Pictures

A new item queues `shopping_picture` on the agent of whoever added it. The job
searches the web (the server's configured search, Brave), lets a model pick the
result most likely to be the item's own product page, and keeps that page's
picture, fetched through `safefetch`, scaled down and stored as the link
preview code keeps one (`linkpreview.FetchImage`). "Find another" runs it again
without the page the current picture came from. A person can upload a picture
instead. Pictures are served from `/api/v1/shopping/pictures/{shoppingItemId}`
to the people the list reaches, so a reader's device never loads anything from
the page's site. With no search configured an item simply has no picture until
somebody uploads one. A picture found after a person uploaded one, or after
another search changed it, is thrown away.

## Reading a list

`PreviewShoppingImport` shows a model pasted text, screenshots or a photo of a
handwritten list and reads it into items: the count apart from the name (a
number that is part of a product's name stays in it), every preference in the
list's own words, the aisle from the section heading, the store from the list's
heading, and whether an item is a store's own product. Nothing is added until
the person presses Add. In a conversation the agent reads a pasted list or a
screenshot itself and calls `add_items` with the items.

## The reminder at a store

When the phone reports an arrival (`afterLocationChange`), the person's own
phones are told what their lists need there: the items for that store and, when
the place's OpenStreetMap category is a grocery-type shop, the items for any
store. The place is the store the person named it, else its name on the map.
Nothing is sent for a store the person asked never to be reminded at, for a
list they muted, for an arrival more than fifteen minutes old, or twice within
three hours at one place. Each person a list reaches chooses the reminder for
themselves.

## The agent

The `shopping` tool has one action per operation. Adding, changing and checking
off do not ask; deleting a list, removing items and sharing are destructive and
ask first, by the maintainer's choice.
