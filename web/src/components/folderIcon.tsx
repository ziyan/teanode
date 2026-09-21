import {
  ArchiveIcon,
  DraftsIcon,
  FolderIcon,
  InboxIcon,
  JunkIcon,
  PriorityIcon,
  SentIcon,
  StarIcon,
  TrashIcon,
} from './icons'

// The icon of a mailbox folder: one per built-in kind, a plain folder for the
// ones people make, and a star for the view of every flagged message.
export function FolderKindIcon({ kind, size }: { kind?: string | null; size?: number }) {
  switch (kind) {
    case 'inbox':
      return <InboxIcon size={size} />
    case 'drafts':
      return <DraftsIcon size={size} />
    case 'sent':
      return <SentIcon size={size} />
    case 'archive':
      return <ArchiveIcon size={size} />
    case 'junk':
      return <JunkIcon size={size} />
    case 'trash':
      return <TrashIcon size={size} />
    case 'starred':
      return <StarIcon size={size} />
    case 'priority':
      return <PriorityIcon size={size} />
    default:
      return <FolderIcon size={size} />
  }
}
