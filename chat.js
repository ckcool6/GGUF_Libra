
// 初始化配置
marked.setOptions({
    highlight: (code, lang) => {
        if (lang && hljs.getLanguage(lang)) return hljs.highlight(code, { language: lang }).value;
        return hljs.highlightAuto(code).value;
    },
    breaks: true, gfm: true
});

const chatBox = document.getElementById('chat-box');
const input = document.getElementById('user-input');
const loading = document.getElementById('ai-loading-template');
const sendBtn = document.getElementById('send-btn');

function toggleDarkMode() {
    const isDark = document.documentElement.classList.toggle('dark-mode-active');
    document.getElementById('mode-icon').innerHTML = isDark
        ? `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 3v1m0 16v1m9-9h-1M4 12H3m15.364-6.364l-.707.707M6.343 17.657l-.707.707m12.728 0l-.707-.707M6.343 6.343l-.707-.707M12 8a4 4 0 100 8 4 4 0 000-8z"></path>`
        : `<path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20.354 15.354A9 9 0 018.646 3.646 9.003 9.003 0 0012 21a9.003 9.003 0 008.354-5.646z"></path>`;
}

async function send() {
    const text = input.value.trim();
    if (!text || sendBtn.disabled) return;

    sendBtn.disabled = true;
    input.value = '';
    input.style.height = 'auto';

    // 1. 插入用户消息
    chatBox.insertAdjacentHTML('beforeend', `<div class="flex justify-end mb-4"><div class="user-bubble p-4 rounded-2xl max-w-[85%] shadow-sm">${text}</div></div>`);

    // 2. 显示加载动画
    chatBox.appendChild(loading);
    loading.classList.remove('hidden');

    chatBox.scrollTo({ top: chatBox.scrollHeight, behavior: 'smooth' });

    try {
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ message: text })
        });

        // --- 注意：这里不要立即隐藏 loading ---

        const id = 'ai-' + Date.now();
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let full = "";
        let isFirstChunk = true; // 标记是否是第一个数据块
        let aiBubbleDiv = null; // 预定义气泡容器

        while (true) {
            const { done, value } = await reader.read();
            if (done) break;

            const chunk = decoder.decode(value);
            const lines = chunk.split('\n');

            for (const line of lines) {
                if (line.startsWith('data: ') && line !== 'data: [DONE]') {
                    try {
                        const json = JSON.parse(line.substring(6));
                        const content = json.choices[0].delta.content || "";

                        if (content) {
                            full += content;

                            if (isFirstChunk) {
                                // 1. 隐藏“正在思考”
                                loading.classList.add('hidden');

                                // 2. 创建气泡的同时直接填入内容，避免出现空气泡
                                const id = 'ai-' + Date.now();
                                const html = `
                    <div class="flex justify-start mb-4">
                        <div id="${id}" class="ai-bubble p-4 rounded-2xl shadow-sm max-w-[90%] markdown-body">
                            ${marked.parse(full)}
                        </div>
                    </div>`;
                                chatBox.insertAdjacentHTML('beforeend', html);

                                aiBubbleDiv = document.getElementById(id);
                                isFirstChunk = false;
                            } else {
                                // 后续内容正常更新
                                aiBubbleDiv.innerHTML = marked.parse(full);
                            }

                            // 渲染高亮和表情
                            aiBubbleDiv.querySelectorAll('pre code').forEach(el => hljs.highlightElement(el));
                            twemoji.parse(aiBubbleDiv, { folder: 'svg', ext: '.svg' });

                            chatBox.scrollTop = chatBox.scrollHeight;
                        }
                    } catch (e) { }
                }
            }
        }
    } catch (err) {
        loading.classList.add('hidden');
        chatBox.insertAdjacentHTML('beforeend', `<div class="flex justify-start mb-4"><div class="ai-bubble p-4 rounded-2xl bg-red-100 text-red-600">连接错误，请检查后端。</div></div>`);
    } finally {
        loading.classList.add('hidden'); // 保险起见，最后必须隐藏
        sendBtn.disabled = false;
    }
}

function downloadChat() {
    const messages = chatBox.querySelectorAll('.user-bubble, .ai-bubble');
    let content = "--- 聊天记录 ---\n\n";
    messages.forEach(el => {
        const role = el.classList.contains('user-bubble') ? "【用户】" : "【AI】";
        content += `${role}\n${el.innerText.trim()}\n\n`;
    });
    const blob = new Blob([content], { type: 'text/plain;charset=utf-8' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `chat-${Date.now()}.txt`;
    a.click();
}

async function newChat() { if (confirm("清空所有对话？")) { await fetch('/api/new-chat'); location.reload(); } }

window.onload = () => {
    loadHistory();
    input.addEventListener('keydown', e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send(); } });
};

async function loadHistory() {
    try {
        const res = await fetch('/api/history');
        const data = await res.json();
        if (data && data.length > 0) {
            // 清空默认的欢迎语（可选）
            chatBox.innerHTML = '';

            data.forEach(m => {
                const isUser = m.role === 'user';
                const content = isUser ? m.content.replace(/</g, "&lt;").replace(/>/g, "&gt;") : marked.parse(m.content);
                const html = `
            <div class="flex ${isUser ? 'justify-end' : 'justify-start'} mb-4">
                <div class="${isUser ? 'user-bubble' : 'ai-bubble'} p-4 rounded-2xl max-w-[90%] shadow-sm markdown-body">
                    ${content}
                </div>
            </div>`;
                chatBox.insertAdjacentHTML('beforeend', html);
            });

            // 渲染代码高亮和表情
            chatBox.querySelectorAll('pre code').forEach(el => hljs.highlightElement(el));
            twemoji.parse(chatBox, { folder: 'svg', ext: '.svg' });

            // 滚动到底部
            chatBox.scrollTop = chatBox.scrollHeight;
        }
    } catch (e) {
        console.error("加载历史记录失败:", e);
    }
}
