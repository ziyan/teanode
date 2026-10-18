import { BriefcaseIcon, DumbbellIcon, FamilyIcon, GraduationCapIcon, HomeIcon, LocationIcon } from './icons'

// The labels a person can give a place, in the order they are offered. The
// empty label is a place with none. The server accepts exactly these words.
export const PLACE_LABELS = ['home', 'work', 'school', 'family', 'gym'] as const
export type PlaceLabel = (typeof PLACE_LABELS)[number] | ''

// PlaceLabelIcon draws the mark of a label: a house for home, a briefcase for
// work, and a plain pin for a place with no label.
export function PlaceLabelIcon({ placeLabel, size = 16 }: { placeLabel: string; size?: number }) {
  switch (placeLabel) {
    case 'home':
      return <HomeIcon size={size} />
    case 'work':
      return <BriefcaseIcon size={size} />
    case 'school':
      return <GraduationCapIcon size={size} />
    case 'family':
      return <FamilyIcon size={size} />
    case 'gym':
      return <DumbbellIcon size={size} />
    default:
      return <LocationIcon size={size} />
  }
}
