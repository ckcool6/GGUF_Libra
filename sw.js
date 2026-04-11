const CACHE_NAME = 'gemini-v1';
const ASSETS = [
  '/',
  '/index.html',
  'https://cdn.tailwindcss.com',
  'https://unpkg.com/twemoji@latest/dist/twemoji.min.js',
  'https://cdn.jsdelivr.net/npm/marked/marked.min.js',
  'https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/highlight.min.js'
];

// 安装时缓存静态资源
self.addEventListener('install', (e) => {
  e.waitUntil(
    caches.open(CACHE_NAME).then((cache) => cache.addAll(ASSETS))
  );
});

// 拦截请求：优先从缓存读取，节省加载时间
self.addEventListener('fetch', (e) => {
  // API 请求不缓存，直接走网络
  if (e.request.url.includes('/api/')) return;

  e.respondWith(
    caches.match(e.request).then((res) => res || fetch(e.request))
  );
});