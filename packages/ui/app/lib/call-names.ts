/**
 * The name to show somebody in a call by: their username, or for a guest —
 * whose username is generated — the name they gave.
 */
export function shownName(person: { username: string; displayName?: string; guest?: boolean }) {
  return person.guest ? person.displayName || person.username : person.username;
}
