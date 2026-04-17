<script>
  let count = $state(0);
  let doubled = $derived(count * 2);
  $effect(() => console.log(count));
  let { name = "anon" } = $props();
  let raw = $state.raw([]);
  let eager = $state.eager(0);
  let sum = $derived.by(() => count + 1);
  $effect.pre(() => console.log("pre"));
  $effect.root(() => {});
  $effect.tracking();
  $effect.pending();
  let id = $props.id();
  let val = $bindable(0);
  $inspect(count);
  $inspect.trace(count);
  let h = $host();
</script>
