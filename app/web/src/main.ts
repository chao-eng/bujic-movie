import { createApp } from 'vue'
import { createPinia } from 'pinia'
import router from './router'
import './style.css'
import 'vue-sonner/style.css'
import App from './App.vue'
import { initTheme } from './composables/useTheme'

// 先确定主题类再挂载，避免首屏闪白/闪黑。
initTheme()

const app = createApp(App)

app.use(createPinia())
app.use(router)

app.mount('#app')
