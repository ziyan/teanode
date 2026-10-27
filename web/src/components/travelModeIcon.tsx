import { BikeIcon, BoatIcon, BusIcon, CarIcon, PlaneIcon, RunIcon, TrainIcon, WalkIcon } from './icons'

// TravelModeIcon draws the mark of a way of travel. A way the dashboard does
// not know (the server may learn one first) is drawn as a car, the commonest.
export function TravelModeIcon({ travelMode, size = 16 }: { travelMode: string; size?: number }) {
  switch (travelMode) {
    case 'walk':
      return <WalkIcon size={size} />
    case 'run':
      return <RunIcon size={size} />
    case 'bike':
      return <BikeIcon size={size} />
    case 'bus':
      return <BusIcon size={size} />
    case 'train':
      return <TrainIcon size={size} />
    case 'boat':
      return <BoatIcon size={size} />
    case 'plane':
      return <PlaneIcon size={size} />
    default:
      return <CarIcon size={size} />
  }
}
