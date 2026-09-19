const CACHE = 'selah-v1';
const STATIC_ASSETS = [
  '/static/css/style.css?v=35',
  '/static/manifest.json',
  '/static/app-icon-192-dark.png',
  '/static/apple-touch-icon.png',
];

self.addEventListener('install', e => {
  e.waitUntil(caches.open(CACHE).then(c => c.addAll(STATIC_ASSETS)));
  self.skipWaiting();
});

self.addEventListener('activate', e => {
  e.waitUntil(
    caches.keys().then(keys =>
      Promise.all(keys.filter(k => k !== CACHE).map(k => caches.delete(k)))
    ).then(() => clients.claim())
  );
});

self.addEventListener('fetch', e => {
  // Static assets: cache-first
  if (e.request.url.includes('/static/')) {
    e.respondWith(
      caches.match(e.request).then(cached => cached || fetch(e.request))
    );
    return;
  }
  // Everything else: network-first (app requires auth)
  e.respondWith(fetch(e.request).catch(() => caches.match(e.request)));
});
