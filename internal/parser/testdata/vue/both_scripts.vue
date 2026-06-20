<script>
export const componentName = 'MyWidget'
</script>

<script setup lang="ts">
function greet(who: string): string {
  return 'Hello ' + who
}
</script>

<template>
  <div>{{ greet('world') }}</div>
</template>
