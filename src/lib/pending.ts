// Files picked on the landing page, handed to the Room that is created for them.
// Files can't go through sessionStorage or the URL, so they live in memory
// across the client-side navigation.
let pending: File[] = []

export function setPendingFiles(files: File[]) {
  pending = files
}

export function takePendingFiles(): File[] {
  const files = pending
  pending = []
  return files
}
