import { Link } from 'react-router-dom'

// The two tile shapes an overview is built from, after the reference: a band
// of stats that answer "how many", then a band of destinations that answer
// "where do I go".
//
// They are separate shapes because they answer different questions. A number
// wants to be read at a glance and is mostly whitespace; a destination wants
// its name and a line saying what is behind it. Making one component do both
// produces a tile that is bad at each.

// Section is the small heading above a band: an icon, a word in caps, and
// optionally something on the right — a period the numbers cover, a link to
// the full list.
export function Section({
  icon,
  label,
  aside,
  children,
}: {
  icon: React.ReactNode
  label: string
  aside?: React.ReactNode
  children: React.ReactNode
}) {
  return (
    <section className="band">
      <h3 className="band-heading">
        <span className="band-icon">{icon}</span>
        {label}
        {aside && <span className="band-aside">{aside}</span>}
      </h3>
      <div className="tile-grid">{children}</div>
    </section>
  )
}

// StatTile is one number and what it counts. The label is above it and the
// detail below, so a row of them reads down the middle at a glance.
export function StatTile({
  label,
  value,
  unit,
  detail,
  icon,
  to,
}: {
  label: string
  value: React.ReactNode
  unit?: string
  detail?: React.ReactNode

  // The mark in the corner, for a tile that wants attention. This is the one
  // thing on a tile that is coloured: the number is not, because a count is
  // not a verdict — a rejection count is the count of a thing that happened,
  // and zero of them is not good news.
  icon?: React.ReactNode

  to?: string
}) {
  const body = (
    <>
      <span className="tile-label">{label}</span>
      <span className="tile-value">
        {value}
        {unit && <span className="tile-unit">{unit}</span>}
      </span>
      {detail && <span className="tile-detail">{detail}</span>}
      {icon && <span className="tile-corner">{icon}</span>}
    </>
  )

  // A tile with somewhere to go is a link, all of it. A tile that is only a
  // number is not pretending to be one.
  return to ? (
    <Link className="tile stat" to={to}>
      {body}
    </Link>
  ) : (
    <div className="tile stat">{body}</div>
  )
}

// ResourceTile is a destination: an icon, its name, and a line about what is
// behind it.
export function ResourceTile({
  icon,
  title,
  detail,
  to,
}: {
  icon: React.ReactNode
  title: string
  detail: string
  to: string
}) {
  return (
    <Link className="tile resource" to={to}>
      <span className="tile-corner-icon">{icon}</span>
      <strong>{title}</strong>
      <span className="tile-detail">{detail}</span>
    </Link>
  )
}
