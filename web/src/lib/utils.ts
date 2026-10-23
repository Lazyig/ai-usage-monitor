import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

// The theme in index.css replaces Tailwind's scales with Fluent token names, so
// tailwind-merge has to learn them: otherwise `text-300` reads as a colour and
// silently drops a `text-fg1` next to it.
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: ['100', '200', '300', '400', '500', '600', 'hero700'],
      leading: ['200', '300', '400', '500', '600', 'hero700'],
      'font-weight': ['regular', 'semibold'],
      radius: ['sm', 'md', 'lg', 'xl'],
      shadow: ['2', '4', '16', '64'],
    },
  },
})

/** shadcn's class combiner: conditional classes, later utilities win. */
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
