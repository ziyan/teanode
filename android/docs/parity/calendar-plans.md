# Parity: calendar and plans

The calendar, reminders, goals, ideas, knowledge and journal areas of the iPhone app, ported to Android.
Marks: done, partly (what is left is named), not ported (why).

All Android paths are under `app/src/main/java/com/teanode/app/` (screens) and
`kit/src/main/kotlin/com/teanode/kit/` (logic). Every screen has demo data and Roborazzi pictures
(light, dark, and the largest text for dense screens) under `app/src/test/java/com/teanode/app/<area>/`.

## Kit (logic, JUnit 5 tests in `kit/src/test/kotlin/com/teanode/kit/<area>/`)

| iOS file | Android | Mark |
| --- | --- | --- |
| Calendar.swift | calendar/CalendarModels.kt, CalendarApi.kt, CalendarDocuments.kt, ServerTime.kt | done |
| CalendarMonth.swift | calendar/CalendarMonth.kt (CalendarRules replaces the iOS Calendar value; week start and minimum days are inputs) | done |
| CalendarRepeat.swift | calendar/CalendarRepeat.kt (parse, write, presets, sentence, edited rule) | done |
| FindATime.swift | calendar/FindATime.kt | done |
| Goals.swift | goals/Goals.kt | done |
| Ideas.swift | ideas/Ideas.kt | done |
| Knowledge.swift | knowledge/KnowledgeModels.kt, KnowledgeApi.kt | done |
| KnowledgeRecords.swift | knowledge/KnowledgeRecords.kt (dashboard path to record target) | done |
| KnowledgeSharing.swift | knowledge/KnowledgeSharing.kt | done |
| Journal.swift | journal/JournalApi.kt | done |
| JournalModels.swift | journal/JournalModels.kt (null lists decoded as empty) | done |
| JournalDays.swift | journal/JournalDays.kt (weeks, month grid, evening time, money, polling) | done |
| JournalNarrativeText.swift | journal/JournalNarrativeText.kt (mentions, record opening) | done |

## Calendar and reminders

| iOS file | Android | Mark |
| --- | --- | --- |
| CalendarModel.swift | calendar/CalendarModel.kt, RemindersModel.kt, CalendarModelHost.kt, EventDraft.kt (activity-scoped, per account) | done |
| CalendarView.swift: month grid, agenda, day timeline with overlap columns, five day, week | CalendarMonthViews.kt, CalendarDayViews.kt, CalendarDayScreen.kt, CalendarSection.kt | done |
| CalendarView.swift: event reader and editor, new event from a time | EventEditorScreen.kt, DateTimeFields.kt | done |
| CalendarView.swift: search | CalendarSearchScreen.kt | done |
| CalendarView.swift: calendars list, colors, new and delete | CalendarListScreen.kt | done |
| CalendarView.swift: pending change box | CalendarDayViews.kt (PendingChangeBox) | done |
| CalendarView.swift: lunar dates | kit CalendarMonth.kt (LunarDates), month cells | done |
| CalendarRepeatViews.swift | CustomRepeatScreen.kt, CalendarRepeatWords.kt | done |
| CalendarSharingViews.swift: people, levels, circle calendars offer | CalendarShareScreen.kt, CalendarListScreen.kt (a row opens the exact circle in Settings) | done |
| FindATimeViews.swift | FindATimeScreen.kt | done |
| RemindersView.swift | RemindersViews.kt (To do and Done, tick, editor, due words) | done |

## Goals, ideas

| iOS file | Android | Mark |
| --- | --- | --- |
| GoalsView.swift: list, detail, schedule picker, starter, drop confirmation | goals/GoalsSection.kt, GoalDetailScreen.kt, GoalSchedulePickerScreen.kt, GoalStarterScreen.kt | done |
| GoalsView.swift: Ask agent hand-off | goals/ChatHandoff.kt (Chats' ChatsHooks.openConversation) | done |
| IdeasView.swift | ideas/IdeasSection.kt | done (the try-it hand-off uses ChatHandoff, same caveat) |

## Knowledge

| iOS file | Android | Mark |
| --- | --- | --- |
| KnowledgeViews.swift: folders, search, pinned, pages, facts, links, neighbours | knowledge/KnowledgeFolderScreen.kt, KnowledgePageScreen.kt, KnowledgePageModel.kt | done |
| KnowledgeViews.swift: editors (page, fact, links, contact, page picker) | knowledge/KnowledgeEditors.kt | done |
| KnowledgeViews.swift: recall, documents, document reader | knowledge/KnowledgeSearchScreens.kt | done |
| KnowledgeViews.swift: linked records open their screens | knowledge/KnowledgeCommon.kt (openRecord to the real routes of Mail, Contacts, Calendar, Chats, Finance, Recordings) | done |
| KnowledgeViews.swift: file and picture previews of attachments | knowledge/KnowledgePageScreen.kt (RemotePicture), knowledge/KnowledgeFiles.kt (PDF first page and viewer with PdfRenderer; other files open in another app) | done |
| KnowledgeViews.swift: markdown | KnowledgeCommon.kt (MarkdownText uses Chats' MarkdownView: tables, images, lists, code) | done |
| KnowledgeSharingViews.swift | knowledge/SharedKnowledgeScreens.kt (shared with you, shared page, share and preview) | done |
| The in-memory card shown elsewhere | knowledge/InMemory.kt | done, offered to other areas for reuse |

## Journal

| iOS file | Android | Mark |
| --- | --- | --- |
| JournalViews.swift | journal/JournalSection.kt (search, today card, week groups, day picker, zoom) | done |
| JournalModel.swift | journal/JournalModels.kt, JournalModelHost.kt | done |
| JournalDayViews.swift | journal/JournalDayScreen.kt (visits map with Location's TeaNodeMap, places open the day in Location) | done |
| JournalMoodViews.swift | journal/JournalParts.kt (faces, picker, bars, strips) | done |
| JournalParts.swift | journal/JournalParts.kt, JournalIcons.kt | done |
| JournalPeriodViews.swift | journal/JournalPeriodScreen.kt (week list, month grid, year months, totals, chapters) | done |
| JournalSettingsViews.swift | journal/JournalSettingsScreen.kt | done |
| JournalStoryViews.swift | journal/JournalStoryRows.kt (story, corrections, write again, remove) | done |
| JournalText.swift | journal/JournalText.kt, res journal_strings.xml and journal_plurals.xml | done |
| JournalWorkViews.swift | journal/JournalStoryRows.kt, JournalDayScreen.kt (projects, shipped work, sessions) | done |
| JournalWriteViews.swift | journal/JournalWriteScreens.kt, JournalDictation.kt (Chats' Dictation) | done |
| JournalDemo.swift | journal/JournalDemo.kt | done |

## Not ported

- Previews of office files inside the app: they open in another app (the phone's viewer), as the brief allowed.
- The Ask agent button of the iOS goal screen goes through Chats' hook and sends the opening request once.

## Hand-offs to other areas

- `ChatHandoff` calls `ChatsHooks.openConversation`, a small additive hook added to Chats' ConversationScreen.kt (it sends an
  opening request once as the conversation opens).
- A calendar event known only by its identifier opens through `CalendarRoutes.UNKNOWN_CALENDAR`, which finds its calendar.
- Contacts' demo answerer for `AgentGraphPage` now defers to Knowledge for the whole-page query.

## Not verified

Everything ran against demo data and Robolectric. Not verified against a real server, a phone or an emulator: live
subscriptions and polling of pending stories, photo loading, notifications for the evening question, and the date and
time pickers on a device.
