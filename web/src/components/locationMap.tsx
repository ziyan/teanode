import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import * as maplibregl from 'maplibre-gl/dist/maplibre-gl-csp.js'
import 'maplibre-gl/dist/maplibre-gl.css'

import { authorization } from '../api'
import { useTranslation } from '../i18n/i18n'
import { Avatar } from './avatar'
import { PlaceLabelIcon } from './placeLabelIcon'

// The map under the Location page: the people it shows as their pictures, the
// places as a small marker with the mark of their label, how sure a position is
// as a translucent circle, and a day's path as a line.
//
// The dashboard's content security policy takes nothing from anywhere but the
// server, so three things are not the defaults. The style, the tiles, the
// glyphs and the sprites all come through the server (/api/v1/map/...), which
// fetches them from the configured map host and rewrites the style's addresses
// to itself. MapLibre's build for such pages is used, and its worker is a file
// the dashboard's own build copies to /assets, because the usual build makes
// its worker from a blob: address. And the markers are ordinary elements, not
// drawn by the map.

// Where the server gives the map's style, and where the dashboard serves the
// worker (webpack.config.js copies it there).
const MAP_STYLE_PATH = '/api/v1/map/styles/liberty'
const MAP_WORKER_PATH = '/assets/maplibre-gl-csp-worker.js'

maplibregl.setWorkerUrl(MAP_WORKER_PATH)

export type MapPerson = {
  userId: string
  name: string
  avatarUrl?: string | null
  avatarColorIndex: number
  latitudeDegrees: number
  longitudeDegrees: number
  accuracyMeters: number
  isSelected: boolean
}

export type MapPlace = {
  id: string
  placeName: string
  placeLabel: string
  latitudeDegrees: number
  longitudeDegrees: number
  radiusMeters: number
  // The radius of a selected place is drawn as a circle; the others are marks.
  isSelected?: boolean
}

export type MapPathPoint = { latitudeDegrees: number; longitudeDegrees: number }

const SOURCE_CIRCLES = 'location-circles'
const SOURCE_PATH = 'location-path'

// Past this a position's accuracy is a statement that the position is not
// known, and a circle across a country says nothing a person can use.
const MOST_ACCURACY_CIRCLE_METERS = 5000

const EARTH_RADIUS_METERS = 6378137
const CIRCLE_STEP_COUNT = 48

// circleRing is the ring of a circle on the ground around a point, as the
// longitude and latitude pairs a polygon is drawn from. Flat-earth accurate,
// which is exact enough at the few kilometers a position or a place spans.
function circleRing(latitudeDegrees: number, longitudeDegrees: number, radiusMeters: number): number[][] {
  const latitudeRadians = (latitudeDegrees * Math.PI) / 180
  const ring: number[][] = []
  for (let step = 0; step <= CIRCLE_STEP_COUNT; step++) {
    const bearingRadians = (step / CIRCLE_STEP_COUNT) * 2 * Math.PI
    const northMeters = Math.cos(bearingRadians) * radiusMeters
    const eastMeters = Math.sin(bearingRadians) * radiusMeters
    ring.push([
      longitudeDegrees + ((eastMeters / (EARTH_RADIUS_METERS * Math.cos(latitudeRadians))) * 180) / Math.PI,
      latitudeDegrees + ((northMeters / EARTH_RADIUS_METERS) * 180) / Math.PI,
    ])
  }
  return ring
}

type CircleFeatureCollection = {
  type: 'FeatureCollection'
  features: {
    type: 'Feature'
    properties: { circleKind: 'accuracy' | 'radius' }
    geometry: { type: 'Polygon'; coordinates: number[][][] }
  }[]
}

function circleFeatures(people: MapPerson[], places: MapPlace[]): CircleFeatureCollection {
  const features: CircleFeatureCollection['features'] = []
  for (const person of people) {
    if (person.accuracyMeters > 0 && person.accuracyMeters <= MOST_ACCURACY_CIRCLE_METERS) {
      features.push({
        type: 'Feature',
        properties: { circleKind: 'accuracy' },
        geometry: {
          type: 'Polygon',
          coordinates: [circleRing(person.latitudeDegrees, person.longitudeDegrees, person.accuracyMeters)],
        },
      })
    }
  }
  for (const place of places) {
    if (place.isSelected && place.radiusMeters > 0) {
      features.push({
        type: 'Feature',
        properties: { circleKind: 'radius' },
        geometry: {
          type: 'Polygon',
          coordinates: [circleRing(place.latitudeDegrees, place.longitudeDegrees, place.radiusMeters)],
        },
      })
    }
  }
  return { type: 'FeatureCollection', features }
}

type Slot = { key: string; element: HTMLButtonElement }

// LocationMap draws the people, places and path it is given and fits the view
// to them whenever what is shown moves. Choosing a marker calls back; the page
// decides what choosing means.
export function LocationMap({
  people,
  places,
  path = [],
  onSelectPerson,
  onSelectPlace,
}: {
  people: MapPerson[]
  places: MapPlace[]
  path?: MapPathPoint[]
  onSelectPerson?: (userId: string) => void
  onSelectPlace?: (placeId: string) => void
}) {
  const { t } = useTranslation()
  const container = useRef<HTMLDivElement>(null)
  const mapRef = useRef<maplibregl.Map | null>(null)
  const markersRef = useRef(new Map<string, { marker: maplibregl.Marker; element: HTMLButtonElement }>())
  const [isStyleReady, setStyleReady] = useState(false)
  const [slots, setSlots] = useState<Slot[]>([])

  // The latest callbacks, read when a marker is pressed: a marker's element
  // outlives the render that made it.
  const callbacks = useRef({ onSelectPerson, onSelectPlace })
  callbacks.current = { onSelectPerson, onSelectPlace }

  useEffect(() => {
    if (!container.current) {
      return
    }
    const map = new maplibregl.Map({
      container: container.current,
      style: new URL(MAP_STYLE_PATH, window.location.origin).toString(),
      center: [0, 20],
      zoom: 1.5,
      attributionControl: false,
      // The dashboard framed by the browser extension signs in with a header
      // rather than a cookie, and the server's map answers only the signed in.
      transformRequest: (url) => (url.startsWith(window.location.origin) ? { url, headers: authorization() } : { url }),
    })
    map.addControl(
      new maplibregl.AttributionControl({
        compact: true,
        customAttribution: [
          '<a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener noreferrer">© OpenStreetMap contributors</a>',
          '<a href="https://openfreemap.org" target="_blank" rel="noopener noreferrer">OpenFreeMap</a>',
        ],
      }),
    )
    map.addControl(new maplibregl.NavigationControl({ showCompass: false }), 'top-right')
    map.on('load', () => {
      map.addSource(SOURCE_CIRCLES, { type: 'geojson', data: { type: 'FeatureCollection', features: [] } })
      map.addSource(SOURCE_PATH, { type: 'geojson', data: { type: 'FeatureCollection', features: [] } })
      map.addLayer({
        id: 'location-circles-fill',
        type: 'fill',
        source: SOURCE_CIRCLES,
        paint: {
          'fill-color': ['match', ['get', 'circleKind'], 'radius', '#729d39', '#2f80ed'],
          'fill-opacity': 0.18,
        },
      })
      map.addLayer({
        id: 'location-circles-line',
        type: 'line',
        source: SOURCE_CIRCLES,
        paint: {
          'line-color': ['match', ['get', 'circleKind'], 'radius', '#729d39', '#2f80ed'],
          'line-width': 1.5,
          'line-opacity': 0.7,
        },
      })
      map.addLayer({
        id: 'location-path-line',
        type: 'line',
        source: SOURCE_PATH,
        layout: { 'line-join': 'round', 'line-cap': 'round' },
        paint: { 'line-color': '#2f80ed', 'line-width': 4, 'line-opacity': 0.85 },
      })
      setStyleReady(true)
    })
    mapRef.current = map
    const markers = markersRef.current
    return () => {
      markers.forEach(({ marker }) => marker.remove())
      markers.clear()
      map.remove()
      mapRef.current = null
      setStyleReady(false)
      setSlots([])
    }
  }, [])

  // The circles and the path, once the map has its layers.
  const circles = useMemo(() => circleFeatures(people, places), [people, places])
  useEffect(() => {
    const map = mapRef.current
    if (!map || !isStyleReady) {
      return
    }
    ;(map.getSource(SOURCE_CIRCLES) as maplibregl.GeoJSONSource | undefined)?.setData(circles)
    ;(map.getSource(SOURCE_PATH) as maplibregl.GeoJSONSource | undefined)?.setData({
      type: 'FeatureCollection',
      features:
        path.length < 2
          ? []
          : [
              {
                type: 'Feature',
                properties: {},
                geometry: {
                  type: 'LineString',
                  coordinates: path.map((point) => [point.longitudeDegrees, point.latitudeDegrees]),
                },
              },
            ],
    })
  }, [circles, path, isStyleReady])

  // One marker per person and per place, made when it first appears, moved
  // when it moves and taken away when it is no longer shown.
  useEffect(() => {
    const map = mapRef.current
    if (!map) {
      return
    }
    const markers = markersRef.current
    const wanted = new Map<string, { latitudeDegrees: number; longitudeDegrees: number; label: string }>()
    for (const person of people) {
      wanted.set(`person:${person.userId}`, { ...person, label: person.name })
    }
    for (const place of places) {
      wanted.set(`place:${place.id}`, { ...place, label: place.placeName })
    }
    for (const [key, { marker }] of markers) {
      if (!wanted.has(key)) {
        marker.remove()
        markers.delete(key)
      }
    }
    for (const [key, shown] of wanted) {
      const position: [number, number] = [shown.longitudeDegrees, shown.latitudeDegrees]
      const known = markers.get(key)
      if (known) {
        known.marker.setLngLat(position)
        known.element.setAttribute('aria-label', shown.label)
        continue
      }
      const element = document.createElement('button')
      element.type = 'button'
      element.classList.add(
        'location-marker',
        key.startsWith('person:') ? 'location-marker-person' : 'location-marker-place',
      )
      element.setAttribute('aria-label', shown.label)
      element.addEventListener('click', () => {
        const [kind, identifier] = [key.slice(0, key.indexOf(':')), key.slice(key.indexOf(':') + 1)]
        if (kind === 'person') {
          callbacks.current.onSelectPerson?.(identifier)
        } else {
          callbacks.current.onSelectPlace?.(identifier)
        }
      })
      const marker = new maplibregl.Marker({ element, anchor: 'center' }).setLngLat(position).addTo(map)
      markers.set(key, { marker, element })
    }
    setSlots([...markers.entries()].map(([key, { element }]) => ({ key, element })))
  }, [people, places])

  // Which marker is chosen, as a class on its element: the page is the one that
  // knows, and a marker's element is not React's to render. A class is added
  // and taken off rather than the attribute set, since the map keeps a class
  // of its own on the same element.
  useEffect(() => {
    for (const person of people) {
      markersRef.current.get(`person:${person.userId}`)?.element.classList.toggle('is-selected', person.isSelected)
    }
    for (const place of places) {
      markersRef.current.get(`place:${place.id}`)?.element.classList.toggle('is-selected', Boolean(place.isSelected))
    }
  }, [people, places, slots])

  // Fit the view to what is shown when that changes, not when only a radius or
  // a choice does: moving a slider must not move the map under it.
  const fitKey = useMemo(
    () =>
      [
        ...people.map((person) => `${person.userId}@${person.latitudeDegrees},${person.longitudeDegrees}`),
        ...places.map((place) => `${place.id}@${place.latitudeDegrees},${place.longitudeDegrees}`),
        path.length > 0
          ? `path:${path.length}@${path[0].latitudeDegrees},${path[0].longitudeDegrees}..${path[path.length - 1].latitudeDegrees},${path[path.length - 1].longitudeDegrees}`
          : '',
      ].join('|'),
    [people, places, path],
  )
  useEffect(() => {
    const map = mapRef.current
    if (!map) {
      return
    }
    const points: [number, number][] = [
      ...people.map((person): [number, number] => [person.longitudeDegrees, person.latitudeDegrees]),
      ...places.map((place): [number, number] => [place.longitudeDegrees, place.latitudeDegrees]),
      ...path.map((point): [number, number] => [point.longitudeDegrees, point.latitudeDegrees]),
    ]
    if (points.length === 0) {
      return
    }
    if (points.length === 1) {
      map.jumpTo({ center: points[0], zoom: 14 })
      return
    }
    const bounds = points.reduce((box, point) => box.extend(point), new maplibregl.LngLatBounds(points[0], points[0]))
    map.fitBounds(bounds, { padding: 56, maxZoom: 16, duration: 0 })
    // fitKey stands for the three lists; the lists themselves are new arrays
    // at every render of the page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fitKey])

  // A person or place chosen from a list is brought to the middle.
  const selectedKey =
    people.find((person) => person.isSelected)?.userId ?? places.find((place) => place.isSelected)?.id ?? ''
  useEffect(() => {
    const map = mapRef.current
    const selected = people.find((person) => person.isSelected) ?? places.find((place) => place.isSelected) ?? undefined
    if (!map || !selected) {
      return
    }
    map.easeTo({
      center: [selected.longitudeDegrees, selected.latitudeDegrees],
      zoom: Math.max(map.getZoom(), 14),
      duration: 400,
    })
    // Only when the choice changes, not when the rest of what is shown does.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedKey])

  return (
    <div className="location-map" role="region" aria-label={t('location.mapLabel')}>
      <div className="location-map-canvas" ref={container} />
      {slots.map((slot) => {
        const person = slot.key.startsWith('person:')
          ? people.find((each) => `person:${each.userId}` === slot.key)
          : undefined
        const place = slot.key.startsWith('place:') ? places.find((each) => `place:${each.id}` === slot.key) : undefined
        return createPortal(
          person ? (
            <Avatar name={person.name} avatarUrl={person.avatarUrl} colorIndex={person.avatarColorIndex} size={36} />
          ) : place ? (
            <PlaceLabelIcon placeLabel={place.placeLabel} size={16} />
          ) : null,
          slot.element,
          slot.key,
        )
      })}
    </div>
  )
}
