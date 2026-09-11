const input = document.getElementById('server')
const saved = document.getElementById('saved')
chrome.storage.sync.get('server').then(({ server }) => {
  input.value = server || ''
})
document.getElementById('save').addEventListener('click', async () => {
  const server = input.value.trim().replace(/\/+$/, '')
  await chrome.storage.sync.set({ server })
  saved.textContent = server ? 'Saved.' : 'Cleared.'
})
