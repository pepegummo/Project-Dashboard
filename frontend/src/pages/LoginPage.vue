<script setup lang="ts">
import { ref, reactive, computed } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { useAuthStore } from '@/stores/auth.store';
import { Factory, Building2, Lock, Eye, EyeOff, Loader2 } from 'lucide-vue-next';

const router = useRouter();
const route = useRoute();
const auth = useAuthStore();

const form = reactive({ email: 'admin@acme-foods.com', password: 'Admin@1234' });
const errors = reactive({ email: '', password: '' });
const showPassword = ref(false);
const submitting = ref(false);
const step = ref<'login' | 'org'>('login');
const switchingOrg = ref<string | null>(null);   // which org card's spinner to show

const accessible = computed(() => auth.organizations.filter(o => o.isMember));

function goToApp() {
  const redirect = route.query.redirect as string | undefined;
  router.push(redirect ?? '/dashboards');
}

// Client-side checks before hitting the API. Email must have an @ and a dotted
// domain — good enough; the backend re-validates anyway.
function validate(): boolean {
  errors.email = '';
  errors.password = '';
  const email = form.email.trim();
  if (!email) errors.email = 'Email is required';
  else if (!email.includes('@')) errors.email = "Email must include an '@' (e.g. you@company.com)";
  else if (!/^\S+@\S+\.\S+$/.test(email)) errors.email = 'Enter a valid email address (e.g. you@company.com)';
  if (!form.password.trim()) errors.password = 'Password is required';
  return !errors.email && !errors.password;
}

async function handleLogin() {
  auth.error = null;
  if (!validate()) return;
  submitting.value = true;
  const ok = await auth.login(form.email.trim(), form.password);
  submitting.value = false;
  if (!ok) return;
  // One (or zero) org → straight in; many → let the user pick.
  if (accessible.value.length > 1) step.value = 'org';
  else goToApp();
}

async function selectOrg(org: { id: string; isMember: boolean }) {
  if (!org.isMember || switchingOrg.value) return;
  // Always switch (even to the home org) so the token gets persisted to localStorage.
  switchingOrg.value = org.id;
  try {
    await auth.switchOrg(org.id);
    goToApp();
  } catch (e) {
    auth.error = (e as Error).message;
  } finally {
    switchingOrg.value = null;
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
        <!-- Step 1: credentials -->
        <template v-if="step === 'login'">
          <h2 class="text-lg font-semibold text-white mb-6">Sign in to your account</h2>

          <form @submit.prevent="handleLogin" novalidate class="space-y-4">
            <div>
              <label class="label">Email address</label>
              <input
                v-model="form.email"
                type="email"
                autocomplete="email"
                class="input"
                :class="errors.email && 'border-red-500/50'"
                placeholder="you@company.com"
                @input="errors.email = ''"
              />
              <p v-if="errors.email" class="mt-1 text-xs text-red-400">{{ errors.email }}</p>
            </div>

            <div>
              <label class="label">Password</label>
              <div class="relative">
                <input
                  v-model="form.password"
                  :type="showPassword ? 'text' : 'password'"
                  autocomplete="current-password"
                  class="input pr-10"
                  :class="errors.password && 'border-red-500/50'"
                  placeholder="••••••••"
                  @input="errors.password = ''"
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
              <p v-if="errors.password" class="mt-1 text-xs text-red-400">{{ errors.password }}</p>
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
        </template>

        <!-- Step 2: org picker -->
        <template v-else>
          <h2 class="text-lg font-semibold text-white mb-1">Select organization</h2>
          <p class="text-xs text-gray-500 mb-6">Locked organizations are not available to your account.</p>

          <div class="space-y-2">
            <button
              v-for="org in auth.organizations"
              :key="org.id"
              type="button"
              class="w-full flex items-center gap-3 p-3 rounded-lg bg-surface-200 border border-white/5 transition-colors text-left"
              :class="org.isMember
                ? 'hover:border-primary-500/40 hover:bg-surface-100'
                : 'opacity-50 cursor-not-allowed'"
              :disabled="!org.isMember || !!switchingOrg"
              @click="selectOrg(org)"
            >
              <div class="flex items-center justify-center w-9 h-9 rounded-lg flex-shrink-0"
                   :class="org.isMember ? 'bg-primary-500/10 text-primary-400' : 'bg-surface-100 text-gray-600'">
                <Building2 v-if="org.isMember" class="w-4 h-4" />
                <Lock v-else class="w-4 h-4" />
              </div>
              <div class="min-w-0 flex-1">
                <p class="text-sm font-medium text-white truncate">{{ org.name }}</p>
                <p class="text-xs text-gray-500">{{ org.isMember ? 'Available' : 'No access' }}</p>
              </div>
              <Loader2 v-if="switchingOrg === org.id" class="w-4 h-4 animate-spin text-primary-400" />
            </button>
          </div>

          <div v-if="auth.error" class="flex items-start gap-2 mt-4 p-3 bg-red-500/10 border border-red-500/20 rounded-lg">
            <span class="text-red-400 text-sm">{{ auth.error }}</span>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>
