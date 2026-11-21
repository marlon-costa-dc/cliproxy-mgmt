/**
 * Visible text of static React markup: tags are removed until none remain, so a tag
 * reassembled by an earlier removal cannot survive.
 */
export function markupText(markup: string, separator = ''): string {
  let text = markup;
  let previous: string;
  do {
    previous = text;
    text = text.replace(/<[^>]*>/g, separator);
  } while (text !== previous);
  return text;
}
