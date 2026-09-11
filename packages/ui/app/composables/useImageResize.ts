/**
 * Reduces an image before it is uploaded.
 *
 * Two reasons, both about the person sending it. A photograph from a modern
 * phone is several megabytes, and on the metered mobile data most TDC members
 * use, sending the original to be displayed at 512 pixels wastes their money.
 * And an avatar has a known size — resizing it here means the server is never
 * asked to store a 12-megapixel picture of someone's face.
 *
 * Done in a canvas, so nothing leaves the device until it has been reduced.
 * Anything that is not an image, or that fails to decode, is returned
 * untouched: an upload must not be lost because a convenience did not work.
 */

/** How an image should be reduced. */
export interface ResizeOptions {
  /** The longest edge, in pixels. */
  maxDimension: number;
  /**
   * JPEG quality, 0 to 1. Ignored when the result keeps transparency, which
   * PNG carries and JPEG cannot.
   */
  quality?: number;
  /**
   * Crop to a square before scaling, centred. What an avatar wants, since a
   * portrait photograph otherwise becomes a letterboxed strip.
   */
  square?: boolean;
}

/** Sensible presets, so call sites do not each invent their own numbers. */
export const resizePresets = {
  /** A profile picture: square, small, and the same for everyone. */
  avatar: { maxDimension: 512, quality: 0.9, square: true },
  /**
   * A photograph shared in a conversation. Large enough to look right on a
   * dense display and to zoom into a little, far smaller than the original.
   */
  shared: { maxDimension: 2048, quality: 0.85 },
} as const satisfies Record<string, ResizeOptions>;

export function useImageResize() {
  /**
   * Returns a reduced copy, or the original when it cannot or need not be
   * reduced.
   */
  async function resize(file: File, options: ResizeOptions): Promise<File> {
    if (!canResize(file)) return file;

    let bitmap: ImageBitmap;
    try {
      bitmap = await createImageBitmap(file);
    } catch {
      // A malformed or unsupported image. The server will decide what to do
      // with it; losing the upload here would be worse.
      return file;
    }

    try {
      const { width, height, sourceX, sourceY, sourceWidth, sourceHeight } = fit(
        bitmap.width,
        bitmap.height,
        options,
      );

      // Already small enough, and not being cropped: sending a re-encoded copy
      // would only lose quality.
      if (
        !options.square &&
        bitmap.width <= options.maxDimension &&
        bitmap.height <= options.maxDimension
      ) {
        return file;
      }

      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;

      const context = canvas.getContext("2d");
      if (!context) return file;

      // The browser's own scaler, told to prioritise quality: a downscale done
      // in one step without it is visibly aliased on fine detail.
      context.imageSmoothingEnabled = true;
      context.imageSmoothingQuality = "high";
      context.drawImage(bitmap, sourceX, sourceY, sourceWidth, sourceHeight, 0, 0, width, height);

      // PNG where transparency must survive, JPEG otherwise: a photograph as
      // PNG is several times larger for no visible gain.
      const keepsAlpha = file.type === "image/png" || file.type === "image/webp";
      const outputType = keepsAlpha ? "image/png" : "image/jpeg";

      const blob = await new Promise<Blob | null>((resolve) =>
        canvas.toBlob(resolve, outputType, options.quality ?? 0.85),
      );
      if (!blob) return file;

      // If the "reduced" copy is larger, the original was better. This happens
      // with small or already-optimised images.
      if (blob.size >= file.size && !options.square) return file;

      return new File([blob], renameFor(file.name, outputType), {
        type: outputType,
        lastModified: file.lastModified,
      });
    } finally {
      // ImageBitmap holds decoded pixels; without this they are freed only
      // whenever the collector next runs.
      bitmap.close();
    }
  }

  return { resize, presets: resizePresets };
}

/**
 * Whether reducing this file makes sense.
 *
 * GIF is excluded because a canvas keeps only the first frame, so "resizing" an
 * animation would silently throw the animation away. SVG is excluded because it
 * has no pixels to reduce and is never rendered here anyway.
 */
function canResize(file: File): boolean {
  return ["image/jpeg", "image/png", "image/webp"].includes(file.type);
}

/** Works out the output size and which part of the source to take. */
function fit(sourceWidth: number, sourceHeight: number, options: ResizeOptions) {
  if (options.square) {
    // Centre crop to a square first, so an avatar is not squashed.
    const edge = Math.min(sourceWidth, sourceHeight);
    const size = Math.min(edge, options.maxDimension);

    return {
      width: size,
      height: size,
      sourceX: Math.round((sourceWidth - edge) / 2),
      sourceY: Math.round((sourceHeight - edge) / 2),
      sourceWidth: edge,
      sourceHeight: edge,
    };
  }

  const longest = Math.max(sourceWidth, sourceHeight);
  const scale = longest > options.maxDimension ? options.maxDimension / longest : 1;

  return {
    width: Math.max(1, Math.round(sourceWidth * scale)),
    height: Math.max(1, Math.round(sourceHeight * scale)),
    sourceX: 0,
    sourceY: 0,
    sourceWidth,
    sourceHeight,
  };
}

/** Keeps the name meaningful when the format changed. */
function renameFor(name: string, outputType: string): string {
  const extension = outputType === "image/png" ? ".png" : ".jpg";
  const stem = name.replace(/\.[^./\\]+$/, "");
  return (stem || "image") + extension;
}
