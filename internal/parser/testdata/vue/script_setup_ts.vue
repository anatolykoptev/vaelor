<script setup lang="ts">
import { ref } from 'vue'

interface Props {
  name: string
}

const count = ref(0)

function increment(): void {
  count.value++
}
</script>

<template>
  <button @click="increment">{{ name }}: {{ count }}</button>
</template>
