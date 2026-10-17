import { useAgentIdentity } from '../../agentPreferences'
import { useTranslation } from '../../i18n/i18n'

// How the photo screens speak of the assistant: by the agent's own name when
// the drawer has heard it (as the drawer's own box does: "Ask Tea ..."), and
// as "the assistant" until then or when the agent has none. One place, so the
// viewer's button and the sentence under Describe cannot name it differently.
// A video is a video here, not a photo: what the assistant looks at for one is
// its poster picture.
export function useAssistantWords(): {
  askAbout: (isVideo: boolean) => string
  describeHint: (isVideo: boolean) => string
} {
  const { t } = useTranslation()
  const name = useAgentIdentity().name.trim()
  return name
    ? {
        askAbout: (isVideo) => t(isVideo ? 'photos.askNamedVideo' : 'photos.askNamed', { name }),
        describeHint: (isVideo) => t(isVideo ? 'photos.describeHintNamedVideo' : 'photos.describeHintNamed', { name }),
      }
    : {
        askAbout: (isVideo) => t(isVideo ? 'photos.askAssistantVideo' : 'photos.askAssistant'),
        describeHint: (isVideo) => t(isVideo ? 'photos.describeHintVideo' : 'photos.describeHint'),
      }
}
