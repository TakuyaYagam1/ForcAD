// In development Vite proxies API requests and the native event WebSocket.
// Keeping the same browser origin allows session cookies on localhost and LAN hosts.
export const SERVER_URL = window.location.origin;
export const API_URL = `${SERVER_URL}/api`;
