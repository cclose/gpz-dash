package main

import (
	"bufio"
	"context"
	"fmt"
	"github.com/mum4k/termdash"
	"github.com/mum4k/termdash/align"
	"github.com/mum4k/termdash/cell"
	"github.com/mum4k/termdash/container"
	"github.com/mum4k/termdash/container/grid"
	"github.com/mum4k/termdash/linestyle"
	"github.com/mum4k/termdash/terminal/tcell"
	"github.com/mum4k/termdash/terminal/terminalapi"
	"github.com/mum4k/termdash/widgets/barchart"
	"github.com/mum4k/termdash/widgets/donut"
	"github.com/mum4k/termdash/widgets/gauge"
	"github.com/mum4k/termdash/widgets/segmentdisplay"
	"github.com/mum4k/termdash/widgets/text"
	"log"
	"time"
	"github.com/jacobsa/go-serial/serial"
	"github.com/adrianmo/go-nmea"
)

const redrawInterval = 250 * time.Millisecond

func optArr(opts ...container.Option) []container.Option {
	return opts
}

func main() {
	var t terminalapi.Terminal
	var err error
	if t, err = tcell.New(tcell.ColorMode(terminalapi.ColorMode256)); err != nil {
		panic(err)
	}
	defer t.Close()

	blank, err := text.New()
	if err != nil {
		panic(err)
	}

	clock, err := text.New()
	if err != nil {
		panic(err)
	}


	builder := grid.New()
	builder.Add(
		grid.RowHeightPerc(16,
			grid.ColWidthPerc(33, grid.Widget(clock)),
			grid.ColWidthPerc(33, grid.Widget(blank)),
			grid.ColWidthPerc(33, grid.Widget(blank)),
		),
	)


	segSpeed, err := segmentdisplay.New()
	if err != nil {
		panic(err)
	}

	textSpeed, err := text.New()
	if err != nil {
		panic(err)
	}

	fuel, err := barchart.New(
		barchart.Labels([]string{"Fuel"}),
	)
	if err != nil {
		panic(err)
	}
	_ = fuel.Values([]int{80}, 100, barchart.BarColors([]cell.Color{cell.ColorGreen}))

	tach, err := donut.New(
		donut.StartAngle(225),
		donut.CellOpts(cell.FgColor(cell.ColorMagenta)),
	)
	if err != nil {
		panic(err)
	}
	_ = tach.Percent(67, donut.Label("9500"))

	odo, err := text.New()
	if err != nil {
		panic(err)
	}
	_ = odo.Write("\tOdometer: 37,485\n\tTrip:       124")

	builder.Add(
		grid.RowHeightPerc(68,
			grid.ColWidthPercWithOpts(40, optArr(container.Border(linestyle.Light), container.BorderTitle("Speed")),
				grid.RowHeightPercWithOpts(80, optArr(container.AlignHorizontal(align.HorizontalRight)), grid.Widget(segSpeed)),
				grid.RowHeightPerc(20, grid.Widget(textSpeed)),
			),
			grid.ColWidthPercWithOpts(20, optArr(container.Border(linestyle.Light)),
				grid.ColWidthPerc(50, grid.Widget(blank)),
				grid.ColWidthPercWithOpts(50, optArr(container.Border(linestyle.Light), container.BorderTitle("Fuel")), grid.Widget(fuel)),
			),
			grid.ColWidthPercWithOpts(40, optArr(container.Border(linestyle.Light), container.BorderTitle("RPM")),
				grid.RowHeightPerc(80, grid.Widget(tach)),
				grid.RowHeightPerc(20, grid.Widget(odo)),
			),
		),
	)

	oilTemp, err := gauge.New(gauge.HideTextProgress())
	if err != nil { panic(err) }
	_ = oilTemp.Percent(55, gauge.TextLabel("125F"))
	oilPress, err := gauge.New(gauge.HideTextProgress())
	if err != nil { panic(err) }
	_ = oilPress.Percent(33, gauge.TextLabel("16 PSI"), gauge.Color(cell.ColorYellow))
	battVolt, err := gauge.New(gauge.HideTextProgress())
	if err != nil { panic(err) }
	_ = battVolt.Percent(85, gauge.TextLabel("16.5V"), gauge.Color(cell.ColorRed))

	builder.Add(
		grid.RowHeightPercWithOpts(16, []container.Option{container.AlignVertical(align.VerticalMiddle)},
			grid.ColWidthPercWithOpts(33, []container.Option{ container.Border(linestyle.Light), container.BorderTitle("Oil Temp")},
				grid.Widget(oilTemp),
			),
			grid.ColWidthPercWithOpts(33, []container.Option{ container.Border(linestyle.Light), container.BorderTitle("Oil Press")},
				grid.Widget(oilPress),
			),
			grid.ColWidthPercWithOpts(33, []container.Option{ container.Border(linestyle.Light), container.BorderTitle("Batt Volt")},
				grid.Widget(battVolt),
			),
		),
	)

	gridOpts, err := builder.Build()
	if err != nil {
		panic(err)
	}

	c, err := container.New(t, gridOpts...)
	if err != nil {
		panic(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	quitter := func(k * terminalapi.Keyboard) {
		if k.Key == 'q' || k.Key == 'Q' {
			cancel()
		}
	}

	startClock(ctx, clock)
	go runGPS(ctx, segSpeed, textSpeed)

	_ = termdash.Run(ctx, t, c, termdash.KeyboardSubscriber(quitter), termdash.RedrawInterval(redrawInterval))
}

func cron(ctx context.Context, interval time.Duration, fn func() error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := fn(); err != nil {
				//panic(err)
			}
			case <-ctx.Done():
				return

		}
	}
}

func startClock(ctx context.Context, clock *text.Text) {
	go cron(ctx, time.Second, func() (err error) {
		timeStr := time.Now().Format("15:04:05")
		clock.Reset()
		err = clock.Write(timeStr, text.WriteCellOpts(cell.FgColor(cell.ColorRed), cell.BgColor(cell.ColorCyan)))

		return
	})
}

func runGPS(ctx context.Context, speedometer *segmentdisplay.SegmentDisplay, seedText *text.Text) {
	options := serial.OpenOptions{
		PortName:        "/dev/ttyS0",
		BaudRate:        9600,
		DataBits:        8,
		StopBits:        1,
		MinimumReadSize: 4,
	}
	serialPort, err := serial.Open(options)
	if err != nil {
		log.Fatalf("serial.Open: %v", err)
	}
	defer serialPort.Close()

	reader := bufio.NewReader(serialPort)
	scanner := bufio.NewScanner(reader)

	go cron(ctx, 100 * time.Millisecond, func() (err error) {
		found := false
		for !found && scanner.Scan() {
			scanText := scanner.Text()
			s, err := nmea.Parse(scanText)
			if err == nil {
				switch s.DataType() {
				case nmea.TypeRMC:
					found = true
					data := s.(nmea.RMC)
					_ = speedometer.Write([]*segmentdisplay.TextChunk{segmentdisplay.NewChunk(fmt.Sprintf("%0.0f", data.Speed), segmentdisplay.WriteCellOpts(cell.FgColor(cell.ColorGreen)))})
					seedText.Reset()
					_ = seedText.Write(fmt.Sprintf("GPS Speed: %0.2fn  Lat: %0.4f\n Long: %0.4f\n", data.Speed, data.Latitude, data.Longitude))
					break
				}
			} else {
				return err
			}
		}

		return
	})

	// sleep thread until quit to keep serial open
	select {
		case <-ctx.Done():
			return
	}
}