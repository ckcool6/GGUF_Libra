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

// 修改：完美的暗黑模式切换逻辑
function toggleDarkMode() {
    // 同时切换自定义变量类名与 Tailwind 官方类名
    document.documentElement.classList.toggle('dark-mode-active');
    const isDark = document.documentElement.classList.toggle('dark');
    
    // 写入本地存储备忘录，以便刷新时读取
    localStorage.setItem('theme', isDark ? 'dark' : 'light');
    
    // 刷新图标显示
    updateModeIcon(isDark);
}

// 提取出更新图标的独立函数，方便复用
function updateModeIcon(isDark) {
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

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let full = "";
        let isFirstChunk = true; 
        let aiBubbleDiv = null; 

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
                                loading.classList.add('hidden');

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
                                aiBubbleDiv.innerHTML = marked.parse(full);
                            }

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
        loading.classList.add('hidden'); 
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

async function newChat() { 
    if (confirm("清空所有对话？")) { 
        try {
            await fetch('/api/new-chat'); 
            chatBox.innerHTML = `
                <div class="flex justify-start mb-4">
                    <div class="ai-bubble p-4 rounded-2xl shadow-sm max-w-[90%] markdown-body">
                        你好！有什么我可以帮你的吗？
                    </div>
                </div>`;
        } catch (e) {
            console.error("清空对话失败:", e);
        }
    } 
}

// 修改：初始化逻辑，全面引入对备忘录的检查
window.onload = () => {
    const savedTheme = localStorage.getItem('theme');
    
    // 如果备忘录存着黑夜，或者本地没有存过但系统是黑夜模式
    if (savedTheme === 'dark' || (!savedTheme && window.matchMedia('(prefers-color-scheme: dark)').matches)) {
        document.documentElement.classList.add('dark-mode-active', 'dark');
        updateModeIcon(true);
    } else {
        document.documentElement.classList.remove('dark-mode-active', 'dark');
        updateModeIcon(false);
    }
    
    loadHistory();
    input.addEventListener('keydown', e => { 
        if (e.key === 'Enter' && !e.shiftKey) { 
            e.preventDefault(); 
            send(); 
        } 
    });
};

async function loadHistory() {
    try {
        const res = await fetch('/api/history');
        const data = await res.json();
        if (data && data.length > 0) {
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

            chatBox.querySelectorAll('pre code').forEach(el => hljs.highlightElement(el));
            twemoji.parse(chatBox, { folder: 'svg', ext: '.svg' });

            chatBox.scrollTop = chatBox.scrollHeight;
        }
    } catch (e) {
        console.error("加载历史记录失败:", e);
    }
}