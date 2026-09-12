---
name: news
description: News headlines and article search via NewsAPI.org
secrets:
  - key: NEWSAPI_KEY
    description: API key from newsapi.org
authenticationProfiles:
  newsapi:
    type: apiKey
    header: X-Api-Key
    value: "{{secret:NEWSAPI_KEY}}"
tools:
  - name: news_headlines
    description: Get top news headlines by country and/or category
    type: http
    method: GET
    url: "https://newsapi.org/v2/top-headlines?country={{country}}&category={{category}}&pageSize={{pageSize}}"
    auth: newsapi
    headers:
      Accept: application/json
    timeout: 15
    result: json
    parameters:
      type: object
      properties:
        country:
          type: string
          description: "ISO 3166-1 country code (e.g. us, gb, de, jp). Defaults to us."
          default: "us"
        category:
          type: string
          enum: ["business", "entertainment", "general", "health", "science", "sports", "technology"]
          description: "News category to filter by"
          default: "general"
        pageSize:
          type: integer
          description: "Number of results (1-100, default 10)"
          default: 10
  - name: news_search
    description: Search news articles by keyword
    type: http
    method: GET
    url: "https://newsapi.org/v2/everything?q={{query}}&sortBy={{sortBy}}&pageSize={{pageSize}}"
    auth: newsapi
    headers:
      Accept: application/json
    timeout: 15
    result: json
    parameters:
      type: object
      properties:
        query:
          type: string
          description: "Keywords or phrases to search for"
        sortBy:
          type: string
          enum: ["relevancy", "popularity", "publishedAt"]
          description: "Sort order for results"
          default: "publishedAt"
        pageSize:
          type: integer
          description: "Number of results (1-100, default 10)"
          default: 10
      required: ["query"]
---

Use news_headlines to get the latest top headlines by country and category.
Use news_search to search for articles on any topic.
A `NEWSAPI_KEY` secret must be configured in TeaNode settings — get a free key at https://newsapi.org/register.
