<script setup lang="ts">
import { ref } from 'vue'
import { fetchUser } from './api'

const count = ref(0)

function load(): void {
  fetchUser(count.value)
}
</script>

<template>
  <button @click="load()">{{ format(count) }}</button>
  <ul>
    <li v-for="u in users">{{ u.label() }}</li>
  </ul>
</template>
