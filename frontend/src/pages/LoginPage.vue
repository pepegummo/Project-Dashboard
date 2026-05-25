<script setup lang="ts">
import { ref, reactive } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { useAuthStore } from '@/stores/auth.store';
import { Factory, Eye, EyeOff, Loader2 } from 'lucide-vue-next';

const router = useRouter();
const route = useRoute();
const auth = useAuthStore();

const form = reactive({ email: 'admin@acme-foods.com', password: 'Admin@1234' });
const showPassword = ref(false);
const submitting = ref(false);

async function handleLogin() {
  submitting.value = true;
  const ok = await auth.login(form.email, form.password);
  submitting.value = false;
  if (ok) {
    const redirect = route.query.redirect as string | undefined;
    router.push(redirect ?? '/dashboards');
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-surface bg-grid-pattern px-4">
    <!-- Gradient orb -->
    <div class="absolute top-1/3 left-1/2 -translate-x-1/2 -translate-y-1/2 w-96 h-96 bg-primary-500/10 rounded-full blur-3xl pointer-events-none" />

    <div class="relative w-full max-w-sm">
      <!-- Logo -->
      <div class="text-center mb-8">
        <div class="inline-flex items-center justify-center w-14 h-14 rounded-2xl bg-gradient-to-br from-primary-500 to-accent-cyan mb-4 shadow-glow-blue">
          <Factory class="w-7 h-7 text-white" />
        </div>
        <h1 class="text-2xl font-bold text-white">IotVision</h1>
        <p class="text-sm text-gray-400 mt-1">Industrial AI Dashboard Platform</p>
      </div>

      <!-- Card -->
      <div class="card border border-white/10 p-8">
        <h2 class="text-lg font-semibold text-white mb-6">Sign in to your account</h2>

        <form @submit.prevent="handleLogin" class="space-y-4">
          <div>
            <label class="label">Email address</label>
            <input
              v-model="form.email"
              type="email"
              autocomplete="email"
              required
              class="input"
              placeholder="you@company.com"
            />
          </div>

          <div>
            <label class="label">Password</label>
            <div class="relative">
              <input
                v-model="form.password"
                :type="showPassword ? 'text' : 'password'"
                autocomplete="current-password"
                required
                class="input pr-10"
                placeholder="••••••••"
              />
              <button
                type="button"
                class="absolute right-3 top-1/2 -translate-y-1/2 text-gray-500 hover:text-gray-300 transition-colors"
                @click="showPassword = !showPassword"
              >
                <Eye v-if="!showPassword" class="w-4 h-4" />
                <EyeOff v-else class="w-4 h-4" />
              </button>
            </div>
          </div>

          <!-- Error -->
          <div v-if="auth.error" class="flex items-start gap-2 p-3 bg-red-500/10 border border-red-500/20 rounded-lg">
            <span class="text-red-400 text-sm">{{ auth.error }}</span>
          </div>

          <button
            type="submit"
            class="btn-primary w-full justify-center py-2.5"
            :disabled="submitting"
          >
            <Loader2 v-if="submitting" class="w-4 h-4 animate-spin" />
            <span>{{ submitting ? 'Signing in…' : 'Sign in' }}</span>
          </button>
        </form>

        <!-- Demo hint -->
        <div class="mt-4 p-3 bg-surface-200 rounded-lg text-center">
          <p class="text-xs text-gray-500">Demo credentials pre-filled above</p>
        </div>
      </div>
    </div>
  </div>
</template>
