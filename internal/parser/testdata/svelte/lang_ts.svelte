<script lang="ts">
  interface User { id: number; name: string }
  export function fetchUser(id: number): User {
    return { id, name: '' };
  }
</script>
