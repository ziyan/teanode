// personLabel is how a person is named where one is chosen from a list: the
// name and, in brackets, the username, so two people with the same name can be
// told apart. A person with no name, or whose name is their username, is the
// username alone.
export function personLabel(person: { name?: string | null; username: string }): string {
  const name = person.name?.trim()
  return name && name !== person.username ? `${name} (${person.username})` : person.username
}

// personName is how a person is named once chosen, in a sentence: their name,
// or their username when they have none.
export function personName(person: { name?: string | null; username: string }): string {
  return person.name?.trim() || person.username
}
