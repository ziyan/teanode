// AgentPart is one tab's worth of the operator's agent settings: what
// agents are allowed at all, the providers and models, what they may do
// and how much, the tools and servers they reach, the installed skills.
// One long form was every one of these under each other.
//
// In a module of its own, with nothing imported, because the settings form
// and the integrations page import each other, and a page importing a
// constant from inside that cycle read it before it existed.
export type AgentPart = 'general' | 'models' | 'features' | 'tools' | 'skills'

export const AGENT_PARTS: AgentPart[] = ['general', 'models', 'features', 'tools', 'skills']
