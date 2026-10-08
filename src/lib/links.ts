// Keep in sync with REPO in public/install.sh
export const GITHUB_REPO = 'simhnna/filedrop'
export const REPO_URL = `https://github.com/${GITHUB_REPO}`
export const RELEASES_URL = `${REPO_URL}/releases/latest`
export const releaseAsset = (name: string) => `${RELEASES_URL}/download/${name}`
