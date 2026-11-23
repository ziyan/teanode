# Shopping flow: check off without shopping, review the cart before Done, resume a trip finished by mistake

This ExecPlan is a living document. The sections Progress, Surprises & Discoveries, Decision Log, and Outcomes & Retrospective must be kept up to date as work proceeds. It is maintained in accordance with the ExecPlan rules the maintainer keeps outside this repository; everything a contributor needs is written here.

## Purpose / Big Picture

Shopping lists (docs/subsystems/shopping.md) are checked off only at a store: the list screen has no check, so a person who bought something elsewhere, or already has it, opens a store screen to tick it, which starts a trip and tells everybody the list is shared with. And Done shopping is one tap that buys the cart at once, with no receipt and no way back: a slip of the thumb ends the trip without the receipt that would have remembered the prices.

After this change:

- **A list has checks.** Each item to buy on a list has a round check. A person who is not shopping checks an item off and it is bought now: no trip starts and nobody is told. Unchecking it (soon after) takes the purchase back. While the person is shopping, the same check puts the item in their cart, as the store screen does.
- **Done shopping shows the cart.** It opens the person's cart: every item they put in it, with pictures, the list each is on and what it should come to. Under it, **Snap the receipt** (the camera, or a file on the dashboard) uploads the receipt and then finishes; **Finish without a receipt** asks first. An item can be taken out of the cart there.
- **A finished trip can be resumed.** For three hours after Done, the store screen, the list and the Shopping home offer "You finished shopping at Example Market at 3:41 PM. Resume". Resuming opens the trip again with what it bought back in the cart.
- **A receipt snapped at Done still prices the cart.** Done buys the cart before the receipt is read, so the receipt matcher also considers items bought in the last twelve hours.
- **The store screen moves what is in the cart to the bottom**, under "In the cart", so what is left to find stays on top.

## Progress

- [x] (2026-11-22) Audit of the iPhone screens from CI screenshots, and of the server's trip model.
- [x] (2026-11-22) Server: migration 0280, database operations, GraphQL, client, CLI, tool, tests.
- [x] (2026-11-22) Dashboard.
- [x] (2026-11-22) iPhone app.
- [x] (2026-11-22) Android app.
- [ ] Docs, review, deploy, TestFlight and Android build from integration.

## Decision Log

- Decision: a check on the list is a purchase when the person is not shopping, and a cart check when they are. Rationale: "check items off without starting to shop"; the trip and its notification belong to being at a store. The store screen keeps its check as before.
- Decision: unchecking a purchase made in the last three hours (`models.ShoppingPurchaseUndoableFor`) takes it back: the item was last bought when it was before. Later, unchecking needs it again and keeps the purchase. Rationale: a quick uncheck is a mistake; one days later is "I need it again".
- Decision: a trip is resumable for the same three hours, only the person's latest, only while they have no other trip open, and only when something it bought is still bought. Rationale: the misclick is noticed in the store or on the way out; an older trip is history.
- Decision: snapping the receipt finishes the trip right after the upload, without waiting for the reading. Rationale: reading takes up to two minutes, and the person is at the checkout. The matcher's candidates now include items bought in the last twelve hours (`models.ShoppingReceiptBoughtWithin`), so the prices still land.
- Decision: the section of bought and unwanted items on a list is called "Checked off". Rationale: checking an item now moves it there.

## Interfaces

Migration `0280_shopping_item_bought_by_trip.sql` adds to `shopping_item` the trip that last bought it (`bought_shopping_trip_id`, cleared when the trip is deleted) and when it was last bought before that (`previous_last_bought_at`).

GraphQL (and every client):

- `CheckOffShoppingItem(shoppingItemId: String!, isCheckedOff: Boolean!): ShoppingItem!`: with true, a needed item, or one in the caller's own cart, is bought now (not needed, last bought now); an item in somebody else's cart is left there. With false, a checked-off item is needed again (purchase taken back when within three hours); an item in the caller's own cart goes back to needed.
- `ResumeShopping(shoppingTripId: String!): ShoppingTrip!`: refused (not found) unless the trip is the caller's and finished, and (invalid arguments) when it finished three hours ago or more or the caller has another trip open.
- `GetResumableShoppingTrip: ShoppingTrip` (null when none).
- `ShoppingTrip.boughtItemCount: Int!`: how many items the trip bought are still bought; what resuming puts back.

CLI: `teanode shopping check-off`, `undo-check-off`, `resumable`, `resume`. Tool: `check_off`, `undo_check_off`, `resumable_trip`, `resume_shopping`.

## The screens (every client)

1. **List.** To-buy rows have a leading round check (44 points or more to tap). Tap: shopping (the person has an open trip of their own) puts it in the cart with `CheckShoppingItem`; otherwise `CheckOffShoppingItem(true)` and the row moves to "Checked off" with its check filled. A filled check on a "Checked off" row calls `CheckOffShoppingItem(false)`. A row in the person's own cart shows a filled check and unchecks with `CheckShoppingItem(false)`; a row in somebody else's cart shows their name and its check cannot be tapped. Tapping the rest of the row opens the item, as before. The check answers at once and is put right if the server refuses.
2. **Done shopping** (list and store screens, shown while the person has items in their cart or a trip open) opens **Your cart**: title, "3 items, about $12.40", the items (picture, count and description, list name when more than one, price), each with a filled check that takes it out of the cart. At the bottom: **Snap the receipt** (prominent; camera or photo library on phones, a file on the dashboard), then **Finish without a receipt** (plain), which asks "Finish without a receipt?" / "The 3 items in your cart are marked bought. You can resume this trip for the next three hours." with Finish and Cancel. Closing the sheet keeps shopping. Snap: upload the picture, start reading it (`ReadReceipt`), then `FinishShopping`, close, and say "Done shopping. The receipt is being read; prices are added when it is done." Without: `FinishShopping`, close, say "Done shopping."
3. **Resume.** With `GetResumableShoppingTrip` non-null and no trip of the person's open, the Shopping home, the list (when the trip names it) and the store screen (when its store is the trip's) show one row at the top: "You finished shopping at Example Market at 3:41 PM" (or "You finished shopping at 3:41 PM" with no store) with a **Resume** button and "3 items go back in your cart". Resume calls `ResumeShopping`, reloads, and on the home screen opens that store.
4. **Store.** Items in the cart (anybody's) move under a last section "In the cart"; the rest stay by aisle. The bottom button is **Done shopping** with the count in the person's cart ("Done shopping · 3") once something is in it; before that there is no bottom button, and scanning a receipt is in the screen's menu.
5. **Shopping home.** While the person shops, a row at the top: "Shopping at Example Market · 3 in your cart", which opens the store.

## Validation

`go test ./internal/db -run 'CheckingOff|FinishedTrip|ReceiptStill'`, the migrations' reverse tests, `go test ./internal/cmd -run ShoppingParity`; the dashboard's vitest files under `web/src/pages/shopping/`; TeaNodeKit tests and the iPhone screenshots `shopping-list`, `shopping-store`, `shopping-cart`; the Android kit tests and screenshot tests. Then on the production server: check an item off a list, uncheck it, shop two items, Done, Finish without a receipt, Resume, Done with a receipt.
