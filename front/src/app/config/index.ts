// In development Vite proxies /api and /socket.io to the backend.
// Keeping the same browser origin allows session cookies on localhost and LAN hosts.
export const SERVER_URL = window.location.origin;
export const API_URL = `${SERVER_URL}/api`;
