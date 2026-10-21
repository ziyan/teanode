// What the tests use of the service worker's script.
export function act(action: string, args: Record<string, unknown>): Promise<{ ok: boolean; data?: any; error?: string }>
export function clearScriptedOrigins(): Promise<void>
