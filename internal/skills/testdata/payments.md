---
name: payments
description: A fictional payment program, for the tests of sharing a skill's tools. Pay a merchant with a one-time card, pay an address that answers with a price, and look at or cancel the requests made.
tools:
  - name: pay
    description: Raise a payment request. The card action pays a merchant an amount; the url action pays whatever the address asks, which only the merchant knows; the report action reports a payment that already happened.
    type: workflow
    actionField: action
    amountField: amount_minor
    currencyField: currency
    amountActions: [card]
    parameters:
      type: object
      properties:
        action:
          type: string
          enum: [card, url, report]
          description: What to do
        merchant:
          type: string
          description: Who is paid, for card
        amount_minor:
          type: integer
          description: The amount in the currency's minor unit, for card
        currency:
          type: string
          description: Three letter currency code, for card
        address:
          type: string
          description: The address that asks for a price, for url
      required: ["action"]
    actions:
      card:
        - name: card
          type: shell
          command: ["paycli", "card", "--merchant", "{{merchant}}", "--amount", "{{amount_minor}}", "--currency", "{{currency}}"]
          timeout: 30
      url:
        - name: url
          type: shell
          command: ["paycli", "url", "{{address}}"]
          timeout: 30
      report:
        - name: report
          type: shell
          command: ["paycli", "report", "{{merchant}}"]
          timeout: 30

  - name: spend_requests
    description: Look at one payment request made earlier, cancel it, or list the active or past ones.
    type: workflow
    actionField: action
    scopedIdField: spend_request_id
    parameters:
      type: object
      properties:
        action:
          type: string
          enum: [retrieve, cancel, active, history]
          description: What to do
        spend_request_id:
          type: string
          description: The request, for retrieve and cancel
      required: ["action"]
    actions:
      retrieve:
        - name: retrieve
          type: shell
          command: ["paycli", "retrieve", "{{spend_request_id}}"]
      cancel:
        - name: cancel
          type: shell
          command: ["paycli", "cancel", "{{spend_request_id}}"]
      active:
        - name: active
          type: shell
          command: ["paycli", "active"]
      history:
        - name: history
          type: shell
          command: ["paycli", "history"]

  - name: balance
    description: What is left to spend. Declares no amount.
    type: shell
    command: ["paycli", "balance"]
    parameters:
      type: object
      properties: {}
---

A fictional program for the tests. Nothing here is a real service.
