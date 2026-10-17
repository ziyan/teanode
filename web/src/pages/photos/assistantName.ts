import { useAgentIdentity } from '../../agentPreferences'
import { useTranslation } from '../../i18n/i18n'

// How the photo screens speak of the assistant: by the agent's own name when
// the drawer has heard it (as the drawer's own box does: "Ask Tea ..."), and
// as "the assistant" until then or when the agent has none. One place, so the
// viewer's button and the sentence under Describe cannot name it differently.
export function useAssistantWords(): { askAboutPhoto: string; describeHint: string } {
  const { t } = useTranslation()
  const name = useAgentIdentity().name.trim()
  return name
    ? { askAboutPhoto: t('photos.askNamed', { name }), describeHint: t('photos.describeHintNamed', { name }) }
    : { askAboutPhoto: t('photos.askAssistant'), describeHint: t('photos.describeHint') }
}
