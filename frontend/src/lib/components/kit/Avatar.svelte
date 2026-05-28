<script lang="ts">
  // Avatar circle — colored initials with consistent color hash from email.
  // Uses the SAME theme classes (.avatar-1 .. .avatar-14) defined in
  // frontend/src/themes/_utilities.css that the mail UI uses, so the palette
  // matches mail automatically (and stays matched even though the JS is
  // duplicated — see project_extension_sdk_pattern memory for rationale).

  interface Props {
    /** Email address used as the color-hash seed. */
    email: string
    /** Optional display name. If absent, initials derive from email. */
    name?: string
    /** Density preset. */
    density?: 'micro' | 'compact' | 'standard' | 'large'
    /** Override the density-derived pixel size (rare). */
    size?: number
  }

  const { email, name, density = 'standard', size }: Props = $props()

  // DJB2-style hash. Bit-for-bit the same as mail's getAvatarColor() in
  // ConversationRow.svelte:172-180 so an extension's contact and a mail
  // sender with the same email render the same color.
  function colorClass(seed: string): string {
    let hash = 0
    for (let i = 0; i < seed.length; i++) {
      hash = seed.charCodeAt(i) + ((hash << 5) - hash)
    }
    return `avatar-${(Math.abs(hash) % 14) + 1}`
  }

  function initials(displayName: string | undefined, fallbackEmail: string): string {
    const src = (displayName || fallbackEmail || '').trim()
    if (!src) return '?'
    const parts = src.split(/\s+/).filter(Boolean)
    if (parts.length >= 2) {
      return ((parts[0][0] ?? '') + (parts[parts.length - 1][0] ?? '')).toUpperCase()
    }
    return src[0].toUpperCase()
  }

  // Density → pixel size table. Tuned to match mail UI's density visual weight.
  const DENSITY_SIZE: Record<NonNullable<Props['density']>, number> = {
    micro: 24,
    compact: 28,
    standard: 32,
    large: 40,
  }

  const px = $derived(size ?? DENSITY_SIZE[density])
  const fontPx = $derived(Math.round(px * 0.4))
  const cls = $derived(colorClass(email || ''))
  const text = $derived(initials(name, email))
</script>

<div
  class="rounded-full flex-shrink-0 inline-flex items-center justify-center font-medium {cls}"
  style:width="{px}px"
  style:height="{px}px"
  style:font-size="{fontPx}px"
  aria-hidden="true"
>
  {text}
</div>
