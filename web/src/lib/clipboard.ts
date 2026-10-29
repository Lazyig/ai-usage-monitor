/** Copies `text` to the clipboard where the browser allows it (secure
 * contexts only); elsewhere it does nothing. */
export function copyText(text: string): Promise<void> | undefined {
  return navigator.clipboard?.writeText(text)
}
