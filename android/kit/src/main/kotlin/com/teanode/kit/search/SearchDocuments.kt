package com.teanode.kit.search

/** The GraphQL document of the search across everything. Copied from the iPhone app's Documents (ios/TeaNodeKit), text unchanged. */
object SearchDocuments {
    val SEARCH_EVERYTHING = """
query (${'$'}words: String!, ${'$'}perAreaLimit: Int) {
  SearchEverything(words: ${'$'}words, perAreaLimit: ${'$'}perAreaLimit) {
    words
    areas { searchArea hasMore results { searchArea targetId titleText detailText happenedAt isMain } }
    failedAreas
  }
}
""".trimEnd()

}
