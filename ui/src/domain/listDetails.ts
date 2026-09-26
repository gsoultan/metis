/** The dimmed lines under an item's name in a list. */
export interface ListDetails {
  detail?: string;
  id?: string;
}

/**
 * What a list shows under an item's name: what it belongs to, and in expert
 * mode its ID as well.
 *
 * Expert mode adds a line and never takes one's place. The project list showed
 * a project's ID instead of its organization, so turning it on hid which
 * organization each project was in.
 */
export function listDetails(expert: boolean, detail: string | undefined, id: string): ListDetails {
  return {
    ...(detail ? { detail } : {}),
    ...(expert ? { id } : {}),
  };
}
