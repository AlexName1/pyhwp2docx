package libreoffice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/gotenberg/gotenberg/v8/pkg/gotenberg"
	"github.com/gotenberg/gotenberg/v8/pkg/modules/api"
	libreofficeapi "github.com/gotenberg/gotenberg/v8/pkg/modules/libreoffice/api"
)

// convertRoute returns an [api.Route] which can convert LibreOffice documents
// to PDF.
func convertRoute(libreOffice libreofficeapi.Uno, engine gotenberg.PdfEngine) api.Route {
	return api.Route{
		Method:      http.MethodPost,
		Path:        "/forms/libreoffice/convert",
		IsMultipart: true,
		Handler: func(c echo.Context) error {
			ctx := c.Get("context").(*api.Context)

			// Let's get the data from the form and validate them.
			var (
				inputPaths       []string
				landscape        bool
				nativePageRanges string
				exportFormFields bool
				singlePageSheets bool
				pdfa             string
				pdfua            bool
				nativePdfFormats bool
				merge            bool
				metadata         map[string]interface{}
			)

			err := ctx.FormData().
				MandatoryPaths(libreOffice.Extensions(), &inputPaths).
				Bool("landscape", &landscape, false).
				String("nativePageRanges", &nativePageRanges, "").
				Bool("exportFormFields", &exportFormFields, true).
				Bool("singlePageSheets", &singlePageSheets, false).
				String("pdfa", &pdfa, "").
				Bool("pdfua", &pdfua, false).
				Bool("nativePdfFormats", &nativePdfFormats, true).
				Bool("merge", &merge, false).
				Custom("metadata", func(value string) error {
					if len(value) > 0 {
						err := json.Unmarshal([]byte(value), &metadata)
						if err != nil {
							return fmt.Errorf("unmarshal metadata: %w", err)
						}
					}
					return nil
				}).
				Validate()
			if err != nil {
				return fmt.Errorf("validate form data: %w", err)
			}

			pdfFormats := gotenberg.PdfFormats{
				PdfA:  pdfa,
				PdfUa: pdfua,
			}

			// Alright, let's convert each document to PDF.
			outputPaths := make([]string, len(inputPaths))
			for i, inputPath := range inputPaths {
				outputPaths[i] = ctx.GeneratePath(".pdf")
				options := libreofficeapi.Options{
					Landscape:        landscape,
					PageRanges:       nativePageRanges,
					ExportFormFields: exportFormFields,
					SinglePageSheets: singlePageSheets,
				}

				if nativePdfFormats {
					options.PdfFormats = pdfFormats
				}

				err = libreOffice.Pdf(ctx, ctx.Log(), inputPath, outputPaths[i], options)
				if err != nil {
					if errors.Is(err, libreofficeapi.ErrInvalidPdfFormats) {
						return api.WrapError(
							fmt.Errorf("convert to PDF: %w", err),
							api.NewSentinelHttpError(
								http.StatusBadRequest,
								fmt.Sprintf("A PDF format in '%+v' is not supported", pdfFormats),
							),
						)
					}

					if errors.Is(err, libreofficeapi.ErrMalformedPageRanges) {
						return api.WrapError(
							fmt.Errorf("convert to PDF: %w", err),
							api.NewSentinelHttpError(http.StatusBadRequest, fmt.Sprintf("Malformed page ranges '%s' (nativePageRanges)", options.PageRanges)),
						)
					}

					return fmt.Errorf("convert to PDF: %w", err)
				}
			}

			// So far so good, let's check if we have to merge the PDFs.
			if len(outputPaths) > 1 && merge {
				outputPath := ctx.GeneratePath(".pdf")

				err = engine.Merge(ctx, ctx.Log(), outputPaths, outputPath)
				if err != nil {
					return fmt.Errorf("merge PDFs: %w", err)
				}

				// Only one output path.
				outputPaths = []string{outputPath}
			}

			// Let's check if the client want to convert each PDF to a specific
			// PDF format.
			zeroValued := gotenberg.PdfFormats{}
			if !nativePdfFormats && pdfFormats != zeroValued {
				convertOutputPaths := make([]string, len(outputPaths))

				for i, outputPath := range outputPaths {
					convertInputPath := outputPath
					convertOutputPaths[i] = ctx.GeneratePath(".pdf")

					err = engine.Convert(ctx, ctx.Log(), pdfFormats, convertInputPath, convertOutputPaths[i])
					if err != nil {
						return fmt.Errorf("convert PDF: %w", err)
					}
				}

				// Important: the output paths are now the converted files.
				outputPaths = convertOutputPaths
			}

			// Writes and potentially overrides metadata entries, if any.
			if len(metadata) > 0 {
				for _, outputPath := range outputPaths {
					err = engine.WriteMetadata(ctx, ctx.Log(), metadata, outputPath)
					if err != nil {
						return fmt.Errorf("write metadata: %w", err)
					}
				}
			}

			if len(outputPaths) > 1 {
				// If .zip archive, document.docx -> document.docx.pdf.
				for i, inputPath := range inputPaths {
					outputPath := fmt.Sprintf("%s.pdf", inputPath)

					err = ctx.Rename(outputPaths[i], outputPath)
					if err != nil {
						return fmt.Errorf("rename output path: %w", err)
					}

					outputPaths[i] = outputPath
				}
			}

			// Last but not least, add the output paths to the context so that
			// the API is able to send them as a response to the client.
			err = ctx.AddOutputPaths(outputPaths...)
			if err != nil {
				return fmt.Errorf("add output paths: %w", err)
			}

			return nil
		},
	}
}

// convertRouteDocx returns an [api.Route] which can convert LibreOffice documents
// to DOCX.
func convertRouteDocx(libreOffice libreofficeapi.Uno) api.Route {
	return api.Route{
		Method:      http.MethodPost,
		Path:        "/forms/libreoffice/convert/docx",
		IsMultipart: true,
		Handler: func(c echo.Context) error {
			ctx := c.Get("context").(*api.Context)

			// Let's get the data from the form and validate them.
			var (
				inputPaths       []string
			)

			err := ctx.FormData().
				MandatoryPaths(libreOffice.Extensions(), &inputPaths).
				Validate()
			if err != nil {
				return fmt.Errorf("validate form data: %w", err)
			}

			// Alright, let's convert each document to DOCX.
			outputPaths := make([]string, len(inputPaths))
			for i, inputPath := range inputPaths {
				outputPaths[i] = ctx.GeneratePath(".docx")

				err = libreOffice.Docx(ctx, ctx.Log(), inputPath, outputPaths[i])
				if err != nil {
					return api.WrapError(
						fmt.Errorf("convert to DOCX: %w", err),
						api.NewSentinelHttpError(
							http.StatusInternalServerError,
							"Error",
						),
					)
				}
			}

			if len(outputPaths) > 1 {
				// If .zip archive, document.hwp -> document.hwp.docx.
				for i, inputPath := range inputPaths {
					outputPath := fmt.Sprintf("%s.docx", inputPath)

					err = ctx.Rename(outputPaths[i], outputPath)
					if err != nil {
						return fmt.Errorf("rename output path: %w", err)
					}

					outputPaths[i] = outputPath
				}
			}
			
			// Last but not least, add the output paths to the context so that
			// the API is able to send them as a response to the client.
			err = ctx.AddOutputPaths(outputPaths...)
			if err != nil {
				return fmt.Errorf("add output paths: %w", err)
			}

			return nil
		},
	}
}
