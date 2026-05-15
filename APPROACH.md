# Approach

## Understanding

The app needs to fetch news from a public API and let users browse, search, read, and bookmark articles. Bookmarks should work offline. The focus is on clean architecture, good state management, and testable code.

---

## Architecture

I'll go with Clean Architecture — three layers: presentation (Compose + ViewModels), domain (use cases + models), and data (Retrofit, Room, Paging). Hilt for dependency injection.

The main reason for this split is that the domain layer stays free of Android dependencies, so use cases and repositories can be unit tested on the JVM without an emulator.

---

## How I'm thinking about each feature

**News Feed** — Paging 3 with a `PagingSource` handles pagination, loading, and error states without much manual plumbing. Each card shows title, image, source, date, and description.

**Article Detail** — I'll use a WebView to open the original article URL. Both NewsAPI and GNews truncate the `content` field, so rendering it natively would give an incomplete read. WebView shows the full article as published. Share and "Open in browser" via standard Android intents. Bookmark toggle available here too.

**Search** — A `StateFlow<String>` for the query with `debounce(500ms)` so the API isn't called on every keystroke.

**Bookmarks** — Stored in Room. The DAO returns a `Flow<List<Article>>` so the bookmark screen updates automatically without manual refresh. Bookmark state is visible from both the feed and detail screen.

**Offline** — Bookmarked articles are readable offline. Feed caching is optional and adds complexity, so I'll skip it unless time allows.

---

## State management

Each ViewModel exposes a single `StateFlow<ScreenState>` covering loading, success, and error. No LiveData — StateFlow integrates naturally with Compose and coroutines.

---

## Testing plan

Unit tests for use cases, repositories (MockWebServer), and ViewModels (Turbine for Flow assertions). Compose UI tests for the main screens to verify loading and content states. Room DAOs tested with an in-memory database.
