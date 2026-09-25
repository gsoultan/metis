/**
 * Reading rendered HTML the way a person, or a screen reader, finds things in
 * it: by the text on the screen and by the names of the controls.
 */

const ENTITIES: Record<string, string> = {
  '&amp;': '&',
  '&lt;': '<',
  '&gt;': '>',
  '&quot;': '"',
  '&#x27;': "'",
  '&#39;': "'",
  '&nbsp;': ' ',
};

function decoded(text: string): string {
  return text.replace(/&(?:amp|lt|gt|quot|nbsp|#x27|#39);/g, (entity) => ENTITIES[entity]);
}

/** The text on the screen: markup and styles gone, entities decoded, spaces collapsed. */
export function visibleText(html: string): string {
  const withoutStyles = html.replace(/<style[^>]*>[\s\S]*?<\/style>/g, ' ');
  return decoded(withoutStyles.replace(/<[^>]+>/g, ' ')).replace(/\s+/g, ' ').trim();
}

/** A control's attributes, with a textarea's content as its value. */
export type Control = Record<string, string>;

function attributesOf(tag: string): Control {
  const attributes: Control = {};
  for (const match of tag.matchAll(/\s([\w:-]+)(?:="([^"]*)")?/g)) {
    attributes[match[1]] = decoded(match[2] ?? '');
  }
  return attributes;
}

function controlWithId(html: string, id: string): Control | undefined {
  const escaped = id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const textarea = new RegExp(`<textarea([^>]*\\sid="${escaped}"[^>]*)>([\\s\\S]*?)</textarea>`).exec(html);
  if (textarea) return { ...attributesOf(textarea[1]), value: decoded(textarea[2]) };
  const input = new RegExp(`<(?:input|select|button)([^>]*\\sid="${escaped}"[^>]*)>`).exec(html);
  return input ? attributesOf(input[1]) : undefined;
}

/**
 * The control a label names, or undefined when no label has that text.
 * Every control with that label, in order, is returned by labelledControls.
 */
export function labelledControl(html: string, label: string): Control | undefined {
  return labelledControls(html, label)[0];
}

export function labelledControls(html: string, label: string): Control[] {
  const controls: Control[] = [];
  for (const match of html.matchAll(/<label([^>]*)>([\s\S]*?)<\/label>/g)) {
    const target = attributesOf(match[1]).for;
    if (target === undefined || visibleText(match[2]) !== label) continue;
    const control = controlWithId(html, target);
    if (control) controls.push(control);
  }
  return controls;
}

/** The control whose aria-label is this name, or undefined. */
export function namedControl(html: string, name: string): Control | undefined {
  for (const match of html.matchAll(/<(?:input|textarea|select|button)([^>]*)>/g)) {
    const attributes = attributesOf(match[1]);
    if (attributes['aria-label'] === name) return attributes;
  }
  return undefined;
}
