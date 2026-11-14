package com.teanode.app.search

import com.teanode.app.demo.DemoAnswerer
import com.teanode.app.demo.graphQLField

/** What demo mode answers for search. Most specific first. */
val searchDemoAnswerers: List<DemoAnswerer> = listOf(
    graphQLField(
        "SearchEverything",
        """{"words":"harbor","areas":[
          {"searchArea":"chats","hasMore":false,"results":[{"searchArea":"chats","targetId":"c1","titleText":"","detailText":"We could walk along the harbor on Saturday.","happenedAt":"2030-01-05T09:00:00Z","isMain":true}]},
          {"searchArea":"mail","hasMore":true,"results":[
            {"searchArea":"mail","targetId":"m1","titleText":"Harbor festival tickets","detailText":"Alex Sample","happenedAt":"2030-01-04T09:00:00Z"},
            {"searchArea":"mail","targetId":"m2","titleText":"Re: Harbor cleanup","detailText":"Sam Sample","happenedAt":"2030-01-03T09:00:00Z"}]},
          {"searchArea":"photos","hasMore":true,"results":[{"searchArea":"photos","targetId":"p1","titleText":"harbor.jpg","detailText":""},{"searchArea":"photos","targetId":"p2","titleText":"boats.jpg","detailText":""}]},
          {"searchArea":"documents","hasMore":false,"results":[{"searchArea":"documents","targetId":"d1","titleText":"","detailText":"...the old harbor was closed to boats in the winter..."}]}
        ],"failedAreas":["finance"]}""",
    ),
)
