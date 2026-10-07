// Keep in sync with REPO in public/install.sh
export const GITHUB_REPO = 'simhnna/filedrop'
export const RELEASES_URL = `https://github.com/${GITHUB_REPO}/releases/latest`
export const releaseAsset = (name: string) => `${RELEASES_URL}/download/${name}`
