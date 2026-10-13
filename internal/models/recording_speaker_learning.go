package models

// RecordingLearningView exposes existing ingestion and digest bookkeeping for retained evidence.
// A digested passage has been processed; the count does not establish useful memories.
type RecordingLearningView struct {
	SourceGeneration              int64                 `json:"sourceGeneration"`
	HasAgent                      bool                  `json:"hasAgent"`
	HasStoppedReading             bool                  `json:"hasStoppedReading"`
	KnowledgeSource               *AgentKnowledgeSource `json:"knowledgeSource" graphapi:"nullable"`
	PendingDocumentCount          int64                 `json:"pendingDocumentCount"`
	DigestedDocumentCount         int64                 `json:"digestedDocumentCount"`
	LastPublishedSourceGeneration int64                 `json:"lastPublishedSourceGeneration"`
	IsCurationPending             bool                  `json:"isCurationPending"`
}
