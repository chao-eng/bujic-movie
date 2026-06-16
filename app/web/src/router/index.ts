import { createRouter, createWebHistory } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import LoginPage from '@/pages/LoginPage.vue'
import DashboardPage from '@/pages/DashboardPage.vue'
import MediaLibraryPage from '@/pages/MediaLibraryPage.vue'
import ScrapePage from '@/pages/ScrapePage.vue'
import TransferPage from '@/pages/TransferPage.vue'
import SettingPage from '@/pages/SettingPage.vue'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/login',
      component: LoginPage,
    },
    {
      path: '/',
      component: AppLayout,
      redirect: '/dashboard',
      children: [
        {
          path: 'dashboard',
          component: DashboardPage,
        },
        {
          path: 'library',
          component: MediaLibraryPage,
        },
        {
          path: 'scrape',
          component: ScrapePage,
        },
        {
          path: 'transfer',
          component: TransferPage,
        },
        {
          path: 'settings',
          component: SettingPage,
        },
      ],
    },
  ],
})

// Route guard to check authentication status
router.beforeEach((to, _, next) => {
  const token = localStorage.getItem('token')

  if (to.path === '/login') {
    if (token) {
      next('/dashboard')
    } else {
      next()
    }
  } else {
    if (!token) {
      next('/login')
    } else {
      next()
    }
  }
})

export default router
